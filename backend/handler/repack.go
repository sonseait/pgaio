package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"pgaio/model"
	"pgaio/service"

	"github.com/jackc/pgx/v5/pgxpool"
)

type RepackHandler struct {
	poolMgr *service.PoolManager

	mu      sync.Mutex
	running *repackJob
}

type repackJob struct {
	Table     string    `json:"table"`
	Database  string    `json:"database"`
	StartedAt time.Time `json:"startedAt"`
	cmd       *exec.Cmd
}

func NewRepackHandler(poolMgr *service.PoolManager) *RepackHandler {
	return &RepackHandler{poolMgr: poolMgr}
}

func (h *RepackHandler) getPool(r *http.Request) *pgxpool.Pool {
	db := r.URL.Query().Get("database")
	pool, err := h.poolMgr.GetPool(r.Context(), db)
	if err != nil {
		return h.poolMgr.DefaultPool()
	}
	return pool
}

// GetTables returns bloat statistics for repackable tables.
func (h *RepackHandler) GetTables(w http.ResponseWriter, r *http.Request) {
	pool := h.getPool(r)
	rows, err := pool.Query(r.Context(), `
		SELECT
			schemaname, relname,
			pg_size_pretty(pg_total_relation_size(quote_ident(schemaname)||'.'||quote_ident(relname))) as total_size,
			pg_total_relation_size(quote_ident(schemaname)||'.'||quote_ident(relname)) as total_bytes,
			COALESCE(n_live_tup, 0) as live_tuples,
			COALESCE(n_dead_tup, 0) as dead_tuples,
			CASE WHEN COALESCE(n_live_tup, 0) + COALESCE(n_dead_tup, 0) > 0
				THEN round(COALESCE(n_dead_tup, 0)::numeric / (COALESCE(n_live_tup, 0) + COALESCE(n_dead_tup, 0)) * 100, 1)
				ELSE 0
			END as bloat_pct,
			last_vacuum, last_autovacuum
		FROM pg_stat_user_tables
		ORDER BY n_dead_tup DESC
		LIMIT 100
	`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	defer rows.Close()

	type TableInfo struct {
		Schema         string  `json:"schema"`
		Table          string  `json:"table"`
		TotalSize      string  `json:"totalSize"`
		TotalBytes     int64   `json:"totalBytes"`
		LiveTuples     int64   `json:"liveTuples"`
		DeadTuples     int64   `json:"deadTuples"`
		BloatPct       float64 `json:"bloatPct"`
		LastVacuum     any     `json:"lastVacuum"`
		LastAutovacuum any     `json:"lastAutovacuum"`
	}

	var tables []TableInfo
	for rows.Next() {
		var t TableInfo
		if err := rows.Scan(&t.Schema, &t.Table, &t.TotalSize, &t.TotalBytes,
			&t.LiveTuples, &t.DeadTuples, &t.BloatPct,
			&t.LastVacuum, &t.LastAutovacuum); err != nil {
			continue
		}
		tables = append(tables, t)
	}

	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: tables})
}

// Run triggers pg_repack on a specific table.
func (h *RepackHandler) Run(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Schema   string `json:"schema"`
		Table    string `json:"table"`
		Database string `json:"database"`
		Jobs     int    `json:"jobs"` // parallel index rebuild jobs
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "invalid request"})
		return
	}
	if req.Table == "" {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "table is required"})
		return
	}
	if req.Schema == "" {
		req.Schema = "public"
	}

	h.mu.Lock()
	if h.running != nil {
		h.mu.Unlock()
		writeJSON(w, http.StatusConflict, model.APIResponse{
			Error: fmt.Sprintf("repack already running on %s (started %s)",
				h.running.Table, h.running.StartedAt.Format("15:04:05")),
		})
		return
	}
	h.mu.Unlock()

	tableName := fmt.Sprintf("%s.%s", req.Schema, req.Table)

	// Build pg_repack CLI args
	args := []string{
		"--no-superuser-check",
		"--table", tableName,
	}
	if req.Jobs > 0 {
		args = append(args, "--jobs", fmt.Sprintf("%d", req.Jobs))
	}

	// Determine connection string
	connStr := h.poolMgr.DefaultConnString()
	if req.Database != "" {
		connStr = h.poolMgr.ConnStringForDB(req.Database)
	}
	if connStr != "" {
		args = append(args, "--dbname", connStr)
	}

	cmd := exec.Command("pg_repack", args...)

	h.mu.Lock()
	h.running = &repackJob{
		Table:     tableName,
		Database:  req.Database,
		StartedAt: time.Now(),
		cmd:       cmd,
	}
	h.mu.Unlock()

	go func() {
		log.Printf("[repack] starting pg_repack on %s (args: %s)", tableName, strings.Join(args, " "))
		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("[repack] ❌ pg_repack on %s failed: %v\nOutput: %s", tableName, err, string(output))
		} else {
			log.Printf("[repack] ✅ pg_repack on %s completed\nOutput: %s", tableName, string(output))
		}

		h.mu.Lock()
		h.running = nil
		h.mu.Unlock()
	}()

	writeJSON(w, http.StatusAccepted, model.APIResponse{
		Success: true,
		Data: map[string]string{
			"message": fmt.Sprintf("pg_repack started on %s", tableName),
			"status":  "running",
		},
	})
}

// GetStatus returns current repack job status.
func (h *RepackHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	job := h.running
	h.mu.Unlock()

	if job == nil {
		writeJSON(w, http.StatusOK, model.APIResponse{
			Success: true,
			Data:    map[string]string{"status": "idle"},
		})
		return
	}

	writeJSON(w, http.StatusOK, model.APIResponse{
		Success: true,
		Data: map[string]any{
			"status":    "running",
			"table":     job.Table,
			"database":  job.Database,
			"startedAt": job.StartedAt,
			"elapsed":   time.Since(job.StartedAt).Round(time.Second).String(),
		},
	})
}

// CancelRepack cancels a running repack job.
func (h *RepackHandler) CancelRepack(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	job := h.running
	h.mu.Unlock()

	if job == nil {
		writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: "no repack running"})
		return
	}

	if job.cmd != nil && job.cmd.Process != nil {
		if err := job.cmd.Process.Kill(); err != nil {
			writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: "failed to cancel: " + err.Error()})
			return
		}
	}

	h.mu.Lock()
	h.running = nil
	h.mu.Unlock()

	log.Printf("[repack] ⚠ pg_repack on %s cancelled by user", job.Table)
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: "repack cancelled"})
}
