package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"pgaio/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

type VacuumHandler struct {
	pool *pgxpool.Pool
}

func NewVacuumHandler(pool *pgxpool.Pool) *VacuumHandler {
	return &VacuumHandler{pool: pool}
}

// GetVacuumStats returns vacuum stats for all user tables.
func (h *VacuumHandler) GetVacuumStats(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT schemaname, relname, n_live_tup, n_dead_tup,
		       CASE WHEN n_live_tup > 0 THEN round(n_dead_tup::numeric / n_live_tup * 100, 1) ELSE 0 END as dead_pct,
		       last_vacuum, last_autovacuum, last_analyze, last_autoanalyze,
		       vacuum_count, autovacuum_count
		FROM pg_stat_user_tables
		ORDER BY n_dead_tup DESC
	`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	defer rows.Close()

	type VacuumStat struct {
		Schema          string      `json:"schema"`
		Table           string      `json:"table"`
		LiveTuples      int64       `json:"liveTuples"`
		DeadTuples      int64       `json:"deadTuples"`
		DeadPct         float64     `json:"deadPct"`
		LastVacuum      interface{} `json:"lastVacuum"`
		LastAutovacuum  interface{} `json:"lastAutovacuum"`
		LastAnalyze     interface{} `json:"lastAnalyze"`
		LastAutoanalyze interface{} `json:"lastAutoanalyze"`
		VacuumCount     int64       `json:"vacuumCount"`
		AutovacuumCount int64       `json:"autovacuumCount"`
	}

	var stats []VacuumStat
	for rows.Next() {
		var s VacuumStat
		if err := rows.Scan(&s.Schema, &s.Table, &s.LiveTuples, &s.DeadTuples, &s.DeadPct,
			&s.LastVacuum, &s.LastAutovacuum, &s.LastAnalyze, &s.LastAutoanalyze,
			&s.VacuumCount, &s.AutovacuumCount); err != nil {
			continue
		}
		stats = append(stats, s)
	}

	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: stats})
}

// TriggerVacuum runs VACUUM ANALYZE on a specific table.
func (h *VacuumHandler) TriggerVacuum(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Schema string `json:"schema"`
		Table  string `json:"table"`
		Full   bool   `json:"full"`
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

	tableName := fmt.Sprintf("%q.%q", req.Schema, req.Table)
	vacuumCmd := "VACUUM ANALYZE"
	if req.Full {
		vacuumCmd = "VACUUM FULL ANALYZE"
	}

	go func() {
		log.Printf("[vacuum] starting %s on %s...", vacuumCmd, tableName)
		sql := fmt.Sprintf("%s %s", vacuumCmd, tableName)
		_, err := h.pool.Exec(context.Background(), sql)
		if err != nil {
			log.Printf("[vacuum] %s on %s failed: %v", vacuumCmd, tableName, err)
		} else {
			log.Printf("[vacuum] ✅ %s on %s completed", vacuumCmd, tableName)
		}
	}()

	writeJSON(w, http.StatusAccepted, model.APIResponse{
		Success: true,
		Data: map[string]string{
			"message": fmt.Sprintf("%s started on %s", vacuumCmd, tableName),
			"status":  "running",
		},
	})
}

// GetBloatStats returns estimated table and index bloat.
func (h *VacuumHandler) GetBloatStats(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT
			schemaname, tablename,
			pg_size_pretty(pg_total_relation_size(quote_ident(schemaname)||'.'||quote_ident(tablename))) as total_size,
			pg_total_relation_size(quote_ident(schemaname)||'.'||quote_ident(tablename)) as total_bytes,
			COALESCE(n_dead_tup, 0) as dead_tuples,
			COALESCE(n_live_tup, 0) as live_tuples,
			CASE WHEN COALESCE(n_live_tup, 0) + COALESCE(n_dead_tup, 0) > 0
				THEN round(COALESCE(n_dead_tup, 0)::numeric / (COALESCE(n_live_tup, 0) + COALESCE(n_dead_tup, 0)) * 100, 1)
				ELSE 0
			END as bloat_pct
		FROM pg_stat_user_tables
		WHERE COALESCE(n_dead_tup, 0) > 0
		ORDER BY n_dead_tup DESC
		LIMIT 50
	`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	defer rows.Close()

	type BloatStat struct {
		Schema     string  `json:"schema"`
		Table      string  `json:"table"`
		TotalSize  string  `json:"totalSize"`
		TotalBytes int64   `json:"totalBytes"`
		DeadTuples int64   `json:"deadTuples"`
		LiveTuples int64   `json:"liveTuples"`
		BloatPct   float64 `json:"bloatPct"`
	}

	var stats []BloatStat
	for rows.Next() {
		var s BloatStat
		if err := rows.Scan(&s.Schema, &s.Table, &s.TotalSize, &s.TotalBytes,
			&s.DeadTuples, &s.LiveTuples, &s.BloatPct); err != nil {
			continue
		}
		stats = append(stats, s)
	}

	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: stats})
}
