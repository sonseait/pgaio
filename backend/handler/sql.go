package handler

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"pgaio/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

type HistoryEntry struct {
	Query    string    `json:"query"`
	Time     time.Time `json:"time"`
	Duration float64   `json:"duration"` // ms
	RowCount int       `json:"rowCount"`
	Error    string    `json:"error,omitempty"`
}

type SQLHandler struct {
	pool    *pgxpool.Pool
	mu      sync.Mutex
	history []HistoryEntry
}

func NewSQLHandler(pool *pgxpool.Pool) *SQLHandler {
	return &SQLHandler{pool: pool}
}

func (h *SQLHandler) addHistory(query string, durMs float64, rowCount int, errMsg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.history = append(h.history, HistoryEntry{
		Query:    query,
		Time:     time.Now(),
		Duration: durMs,
		RowCount: rowCount,
		Error:    errMsg,
	})
	if len(h.history) > 100 {
		h.history = h.history[len(h.history)-100:]
	}
}

// GetHistory returns query execution history.
func (h *SQLHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.history == nil {
		h.history = []HistoryEntry{}
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: h.history})
}

// ClearHistory clears query history.
func (h *SQLHandler) ClearHistory(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.history = []HistoryEntry{}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: "history cleared"})
}

// ExecuteSQL runs an arbitrary SQL query and returns results.
func (h *SQLHandler) ExecuteSQL(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "invalid request body"})
		return
	}
	if req.Query == "" {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "query is required"})
		return
	}

	start := time.Now()
	rows, err := h.pool.Query(r.Context(), req.Query)
	if err != nil {
		durMs := float64(time.Since(start).Microseconds()) / 1000
		h.addHistory(req.Query, durMs, 0, err.Error())
		writeJSON(w, http.StatusOK, model.APIResponse{Success: false, Error: err.Error()})
		return
	}
	defer rows.Close()

	// Get column names
	fields := rows.FieldDescriptions()
	columns := make([]string, len(fields))
	for i, f := range fields {
		columns[i] = string(f.Name)
	}

	// Collect rows
	var results []map[string]interface{}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			continue
		}
		row := make(map[string]interface{})
		for i, col := range columns {
			if i < len(values) {
				row[col] = values[i]
			}
		}
		results = append(results, row)
	}

	if results == nil {
		results = []map[string]interface{}{}
	}

	type SQLResult struct {
		Columns  []string                 `json:"columns"`
		Rows     []map[string]interface{} `json:"rows"`
		RowCount int                      `json:"rowCount"`
	}

	durMs := float64(time.Since(start).Microseconds()) / 1000
	h.addHistory(req.Query, durMs, len(results), "")

	writeJSON(w, http.StatusOK, model.APIResponse{
		Success: true,
		Data: SQLResult{
			Columns:  columns,
			Rows:     results,
			RowCount: len(results),
		},
	})
}

// GetSnippets returns common SQL snippets.
func (h *SQLHandler) GetSnippets(w http.ResponseWriter, r *http.Request) {
	type Snippet struct {
		Name     string `json:"name"`
		Category string `json:"category"`
		Query    string `json:"query"`
	}

	snippets := []Snippet{
		{Name: "Active Queries", Category: "monitoring", Query: "SELECT pid, usename, datname, state, query, now() - query_start AS duration FROM pg_stat_activity WHERE state = 'active' AND pid != pg_backend_pid() ORDER BY duration DESC;"},
		{Name: "Table Sizes", Category: "storage", Query: "SELECT schemaname, relname, pg_size_pretty(pg_total_relation_size(schemaname || '.' || relname)) AS size, n_live_tup AS rows FROM pg_stat_user_tables ORDER BY pg_total_relation_size(schemaname || '.' || relname) DESC LIMIT 20;"},
		{Name: "Index Usage", Category: "performance", Query: "SELECT schemaname, relname, indexrelname, idx_scan, idx_tup_read, idx_tup_fetch FROM pg_stat_user_indexes ORDER BY idx_scan DESC LIMIT 20;"},
		{Name: "Unused Indexes", Category: "performance", Query: "SELECT schemaname, relname, indexrelname, pg_size_pretty(pg_relation_size(indexrelid)) AS size FROM pg_stat_user_indexes WHERE idx_scan = 0 ORDER BY pg_relation_size(indexrelid) DESC;"},
		{Name: "Lock Info", Category: "monitoring", Query: "SELECT l.pid, l.locktype, l.mode, l.granted, d.datname, c.relname FROM pg_locks l LEFT JOIN pg_database d ON l.database = d.oid LEFT JOIN pg_class c ON l.relation = c.oid WHERE l.pid != pg_backend_pid() ORDER BY l.pid;"},
		{Name: "Cache Hit Ratio", Category: "performance", Query: "SELECT datname, round(blks_hit * 100.0 / NULLIF(blks_hit + blks_read, 0), 2) AS cache_hit_ratio FROM pg_stat_database WHERE datname NOT LIKE 'template%' ORDER BY datname;"},
		{Name: "Vacuum Stats", Category: "maintenance", Query: "SELECT schemaname, relname, last_vacuum, last_autovacuum, last_analyze, last_autoanalyze, vacuum_count, autovacuum_count FROM pg_stat_user_tables ORDER BY COALESCE(last_autovacuum, '1970-01-01') ASC LIMIT 20;"},
		{Name: "Bloated Tables", Category: "maintenance", Query: "SELECT schemaname, relname, n_dead_tup, n_live_tup, round(n_dead_tup * 100.0 / NULLIF(n_live_tup + n_dead_tup, 0), 2) AS dead_pct FROM pg_stat_user_tables WHERE n_dead_tup > 0 ORDER BY n_dead_tup DESC LIMIT 20;"},
		{Name: "Replication Status", Category: "monitoring", Query: "SELECT client_addr, state, sent_lsn, write_lsn, flush_lsn, replay_lsn FROM pg_stat_replication;"},
		{Name: "Database Sizes", Category: "storage", Query: "SELECT datname, pg_size_pretty(pg_database_size(datname)) AS size FROM pg_database WHERE datistemplate = false ORDER BY pg_database_size(datname) DESC;"},
		{Name: "Long Running Queries", Category: "monitoring", Query: "SELECT pid, usename, datname, state, now() - query_start AS duration, query FROM pg_stat_activity WHERE state != 'idle' AND pid != pg_backend_pid() AND now() - query_start > interval '5 seconds' ORDER BY duration DESC;"},
		{Name: "Connection Count", Category: "monitoring", Query: "SELECT datname, usename, state, count(*) FROM pg_stat_activity GROUP BY datname, usename, state ORDER BY datname, count DESC;"},
		{Name: "Extensions", Category: "info", Query: "SELECT extname, extversion, n.nspname AS schema FROM pg_extension e JOIN pg_namespace n ON e.extnamespace = n.oid ORDER BY extname;"},
		{Name: "All Settings (non-default)", Category: "info", Query: "SELECT name, setting, unit, source FROM pg_settings WHERE source != 'default' ORDER BY source, name;"},
	}

	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: snippets})
}
