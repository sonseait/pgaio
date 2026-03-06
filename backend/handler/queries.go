package handler

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"pgaio/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

type QueriesHandler struct {
	pool *pgxpool.Pool
}

func NewQueriesHandler(pool *pgxpool.Pool) *QueriesHandler {
	return &QueriesHandler{pool: pool}
}

// GetSlowQueries returns top slow queries from pg_stat_statements.
func (h *QueriesHandler) GetSlowQueries(w http.ResponseWriter, r *http.Request) {
	// Check if pg_stat_statements is available
	var exists bool
	h.pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname='pg_stat_statements')`).Scan(&exists)
	if !exists {
		writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: map[string]interface{}{
			"available": false,
			"message":   "pg_stat_statements extension not installed. Install via Extensions page.",
			"queries":   []interface{}{},
		}})
		return
	}

	rows, err := h.pool.Query(r.Context(), `
		SELECT query, calls, round(mean_exec_time::numeric, 2) as mean_ms,
		       round(total_exec_time::numeric, 2) as total_ms, rows,
		       shared_blks_hit, shared_blks_read
		FROM pg_stat_statements
		WHERE query NOT LIKE '%pg_stat_statements%'
		ORDER BY mean_exec_time DESC LIMIT 50
	`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	defer rows.Close()

	type SlowQuery struct {
		Query          string  `json:"query"`
		Calls          int64   `json:"calls"`
		MeanMs         float64 `json:"meanMs"`
		TotalMs        float64 `json:"totalMs"`
		Rows           int64   `json:"rows"`
		SharedBlksHit  int64   `json:"sharedBlksHit"`
		SharedBlksRead int64   `json:"sharedBlksRead"`
	}

	var queries []SlowQuery
	for rows.Next() {
		var q SlowQuery
		if err := rows.Scan(&q.Query, &q.Calls, &q.MeanMs, &q.TotalMs, &q.Rows, &q.SharedBlksHit, &q.SharedBlksRead); err != nil {
			continue
		}
		queries = append(queries, q)
	}

	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: map[string]interface{}{
		"available": true,
		"queries":   queries,
	}})
}

// ExplainQuery runs EXPLAIN (ANALYZE) on a query and returns the plan.
// For parameterized queries ($1, $2...), uses EXPLAIN without ANALYZE.
func (h *QueriesHandler) ExplainQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "invalid request"})
		return
	}

	query := strings.TrimSpace(req.Query)
	if query == "" {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "query is required"})
		return
	}

	// Safety: only allow SELECT/WITH queries for EXPLAIN
	upper := strings.ToUpper(query)
	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "only SELECT/WITH queries can be explained"})
		return
	}

	// Detect parameterized queries ($1, $2...) from pg_stat_statements
	// pgx treats $N as bind parameters, so replace with NULL for planning
	paramRe := regexp.MustCompile(`\$\d+`)
	hasParams := paramRe.MatchString(query)

	var explainSQL string
	mode := "analyzed"
	if hasParams {
		// Replace $1, $2... with NULL so pgx won't expect bind values
		planQuery := paramRe.ReplaceAllString(query, "NULL")
		explainSQL = "EXPLAIN (COSTS, BUFFERS, FORMAT JSON) " + planQuery
		mode = "estimated"
	} else {
		explainSQL = "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) " + query
	}

	var planJSON []byte
	err := h.pool.QueryRow(r.Context(), explainSQL).Scan(&planJSON)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: "explain failed: " + err.Error()})
		return
	}

	var plan interface{}
	json.Unmarshal(planJSON, &plan)

	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: map[string]interface{}{
		"mode": mode,
		"plan": plan,
	}})
}

// ResetStats resets pg_stat_statements statistics.
func (h *QueriesHandler) ResetStats(w http.ResponseWriter, r *http.Request) {
	_, err := h.pool.Exec(r.Context(), "SELECT pg_stat_statements_reset()")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: "statistics reset"})
}
