package handler

import (
	"net/http"

	"pgaio/model"
	"pgaio/service"

	"github.com/jackc/pgx/v5/pgxpool"
)

type IndexesHandler struct {
	poolMgr *service.PoolManager
}

func NewIndexesHandler(poolMgr *service.PoolManager) *IndexesHandler {
	return &IndexesHandler{poolMgr: poolMgr}
}

func (h *IndexesHandler) getPool(r *http.Request) *pgxpool.Pool {
	db := r.URL.Query().Get("database")
	pool, err := h.poolMgr.GetPool(r.Context(), db)
	if err != nil {
		return h.poolMgr.DefaultPool()
	}
	return pool
}

// GetIndexAdvice returns index analysis: missing, unused, and duplicate indexes.
func (h *IndexesHandler) GetIndexAdvice(w http.ResponseWriter, r *http.Request) {
	pool := h.getPool(r)
	result := map[string]any{}

	// Missing indexes (high seq_scan vs idx_scan)
	missingRows, err := pool.Query(r.Context(), `
		SELECT schemaname, relname, seq_scan, idx_scan,
		       seq_scan - COALESCE(idx_scan, 0) as diff,
		       pg_size_pretty(pg_relation_size(quote_ident(schemaname)||'.'||quote_ident(relname))) as size
		FROM pg_stat_user_tables
		WHERE seq_scan > COALESCE(idx_scan, 0) AND seq_scan > 100
		ORDER BY diff DESC LIMIT 30
	`)
	if err == nil {
		type MissingIndex struct {
			Schema  string `json:"schema"`
			Table   string `json:"table"`
			SeqScan int64  `json:"seqScan"`
			IdxScan int64  `json:"idxScan"`
			Diff    int64  `json:"diff"`
			Size    string `json:"size"`
		}
		var missing []MissingIndex
		for missingRows.Next() {
			var m MissingIndex
			if missingRows.Scan(&m.Schema, &m.Table, &m.SeqScan, &m.IdxScan, &m.Diff, &m.Size) == nil {
				missing = append(missing, m)
			}
		}
		missingRows.Close()
		result["missing"] = missing
	}

	// Unused indexes
	unusedRows, err := pool.Query(r.Context(), `
		SELECT schemaname, relname, indexrelname, idx_scan,
		       pg_size_pretty(pg_relation_size(indexrelid)) as size
		FROM pg_stat_user_indexes
		WHERE idx_scan = 0 AND schemaname NOT IN ('pg_catalog')
		ORDER BY pg_relation_size(indexrelid) DESC
		LIMIT 30
	`)
	if err == nil {
		type UnusedIndex struct {
			Schema string `json:"schema"`
			Table  string `json:"table"`
			Index  string `json:"index"`
			Scans  int64  `json:"scans"`
			Size   string `json:"size"`
		}
		var unused []UnusedIndex
		for unusedRows.Next() {
			var u UnusedIndex
			if unusedRows.Scan(&u.Schema, &u.Table, &u.Index, &u.Scans, &u.Size) == nil {
				unused = append(unused, u)
			}
		}
		unusedRows.Close()
		result["unused"] = unused
	}

	// Duplicate indexes
	dupRows, err := pool.Query(r.Context(), `
		SELECT array_agg(indexname)::text as indexes, tablename, indexdef
		FROM pg_indexes
		WHERE schemaname NOT IN ('pg_catalog','information_schema')
		GROUP BY tablename, indexdef
		HAVING COUNT(*) > 1
	`)
	if err == nil {
		type DupIndex struct {
			Indexes  string `json:"indexes"`
			Table    string `json:"table"`
			IndexDef string `json:"indexDef"`
		}
		var dups []DupIndex
		for dupRows.Next() {
			var d DupIndex
			if dupRows.Scan(&d.Indexes, &d.Table, &d.IndexDef) == nil {
				dups = append(dups, d)
			}
		}
		dupRows.Close()
		result["duplicates"] = dups
	}

	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: result})
}
