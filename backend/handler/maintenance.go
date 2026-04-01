package handler

import (
	"net/http"
	"strings"

	"pgaio/model"
	"pgaio/service"
)

type MaintenanceHandler struct {
	poolMgr *service.PoolManager
}

func NewMaintenanceHandler(poolMgr *service.PoolManager) *MaintenanceHandler {
	return &MaintenanceHandler{poolMgr: poolMgr}
}

func (h *MaintenanceHandler) GetPlan(w http.ResponseWriter, r *http.Request) {
	db := r.URL.Query().Get("database")
	profile := r.URL.Query().Get("profile")
	pool, err := h.poolMgr.GetPoolForProfile(r.Context(), db, profile)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: "failed to connect to database: " + err.Error()})
		return
	}

	type Recommendation struct {
		Category string `json:"category"`
		Priority string `json:"priority"`
		Object   string `json:"object"`
		Action   string `json:"action"`
		Reason   string `json:"reason"`
		SQL      string `json:"sql"`
	}

	plan := []Recommendation{}

	vacuumRows, err := pool.Query(r.Context(), `
		SELECT
			schemaname,
			relname,
			n_live_tup,
			n_dead_tup,
			pg_total_relation_size(quote_ident(schemaname)||'.'||quote_ident(relname)) AS total_bytes
		FROM pg_stat_user_tables
		WHERE n_dead_tup > 1000
		ORDER BY n_dead_tup DESC
		LIMIT 20
	`)
	if err == nil {
		defer vacuumRows.Close()
		for vacuumRows.Next() {
			var schema, table string
			var live, dead, totalBytes int64
			if err := vacuumRows.Scan(&schema, &table, &live, &dead, &totalBytes); err == nil {
				deadPct := 0.0
				if live > 0 {
					deadPct = float64(dead) / float64(live) * 100
				}
				action := "VACUUM ANALYZE"
				priority := "medium"
				if deadPct >= 20 && totalBytes >= 100*1024*1024 {
					action = "pg_repack or VACUUM FULL"
					priority = "high"
				}
				plan = append(plan, Recommendation{
					Category: "table",
					Priority: priority,
					Object:   schema + "." + table,
					Action:   action,
					Reason:   "dead tuples and table size suggest reclaimable space",
					SQL:      "VACUUM ANALYZE " + quoteIdent(schema) + "." + quoteIdent(table) + ";",
				})
			}
		}
	}

	indexRows, err := pool.Query(r.Context(), `
		SELECT
			schemaname,
			relname,
			indexrelname,
			idx_scan,
			pg_relation_size(indexrelid) AS index_bytes
		FROM pg_stat_user_indexes
		WHERE idx_scan = 0
		  AND pg_relation_size(indexrelid) > 10 * 1024 * 1024
		ORDER BY pg_relation_size(indexrelid) DESC
		LIMIT 20
	`)
	if err == nil {
		defer indexRows.Close()
		for indexRows.Next() {
			var schema, table, index string
			var scans, size int64
			if err := indexRows.Scan(&schema, &table, &index, &scans, &size); err == nil {
				plan = append(plan, Recommendation{
					Category: "index",
					Priority: "medium",
					Object:   schema + "." + index,
					Action:   "review unused index",
					Reason:   "index has zero scans and consumes meaningful storage",
					SQL:      "DROP INDEX CONCURRENTLY " + quoteIdent(schema) + "." + quoteIdent(index) + ";",
				})
			}
		}
	}

	seqRows, err := pool.Query(r.Context(), `
		SELECT
			schemaname,
			relname,
			seq_scan,
			COALESCE(idx_scan, 0),
			pg_total_relation_size(quote_ident(schemaname)||'.'||quote_ident(relname)) AS total_bytes
		FROM pg_stat_user_tables
		WHERE seq_scan > COALESCE(idx_scan, 0)
		  AND seq_scan > 100
		  AND pg_total_relation_size(quote_ident(schemaname)||'.'||quote_ident(relname)) > 50 * 1024 * 1024
		ORDER BY (seq_scan - COALESCE(idx_scan, 0)) DESC
		LIMIT 20
	`)
	if err == nil {
		defer seqRows.Close()
		for seqRows.Next() {
			var schema, table string
			var seqScan, idxScan, size int64
			if err := seqRows.Scan(&schema, &table, &seqScan, &idxScan, &size); err == nil {
				plan = append(plan, Recommendation{
					Category: "query",
					Priority: "medium",
					Object:   schema + "." + table,
					Action:   "review missing index",
					Reason:   "sequential scans dominate on a larger table",
					SQL:      "-- inspect predicates and create a selective index for " + quoteIdent(schema) + "." + quoteIdent(table),
				})
			}
		}
	}

	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: map[string]any{
		"database":        db,
		"profile":         profile,
		"recommendations": plan,
	}})
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
