package handler

import (
	"net/http"

	"pgaio/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

type TablesHandler struct {
	pool *pgxpool.Pool
}

func NewTablesHandler(pool *pgxpool.Pool) *TablesHandler {
	return &TablesHandler{pool: pool}
}

// GetTableSizes returns all user tables with size breakdown.
func (h *TablesHandler) GetTableSizes(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT schemaname, tablename,
		       pg_total_relation_size(quote_ident(schemaname)||'.'||quote_ident(tablename)) as total_bytes,
		       pg_relation_size(quote_ident(schemaname)||'.'||quote_ident(tablename)) as table_bytes,
		       pg_indexes_size(quote_ident(schemaname)||'.'||quote_ident(tablename)) as index_bytes,
		       GREATEST(pg_total_relation_size(quote_ident(schemaname)||'.'||quote_ident(tablename)) -
		         pg_relation_size(quote_ident(schemaname)||'.'||quote_ident(tablename)) -
		         pg_indexes_size(quote_ident(schemaname)||'.'||quote_ident(tablename)), 0) as toast_bytes,
		       (SELECT reltuples::bigint FROM pg_class WHERE oid = (quote_ident(schemaname)||'.'||quote_ident(tablename))::regclass) as row_estimate
		FROM pg_tables
		WHERE schemaname NOT IN ('pg_catalog','information_schema')
		ORDER BY pg_total_relation_size(quote_ident(schemaname)||'.'||quote_ident(tablename)) DESC
	`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	defer rows.Close()

	type TableSize struct {
		Schema      string `json:"schema"`
		Table       string `json:"table"`
		TotalBytes  int64  `json:"totalBytes"`
		TableBytes  int64  `json:"tableBytes"`
		IndexBytes  int64  `json:"indexBytes"`
		ToastBytes  int64  `json:"toastBytes"`
		RowEstimate int64  `json:"rowEstimate"`
	}

	var tables []TableSize
	for rows.Next() {
		var t TableSize
		if err := rows.Scan(&t.Schema, &t.Table, &t.TotalBytes, &t.TableBytes, &t.IndexBytes, &t.ToastBytes, &t.RowEstimate); err != nil {
			continue
		}
		tables = append(tables, t)
	}

	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: tables})
}
