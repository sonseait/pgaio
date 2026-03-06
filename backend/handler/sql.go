package handler

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"pgaio/model"
	"pgaio/service"

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
	poolMgr *service.PoolManager
	mu      sync.Mutex
	history []HistoryEntry
}

func NewSQLHandler(poolMgr *service.PoolManager) *SQLHandler {
	return &SQLHandler{poolMgr: poolMgr}
}

func (h *SQLHandler) getPool(r *http.Request) *pgxpool.Pool {
	db := r.URL.Query().Get("database")
	pool, err := h.poolMgr.GetPool(r.Context(), db)
	if err != nil {
		return h.poolMgr.DefaultPool()
	}
	return pool
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
		Query    string `json:"query"`
		Database string `json:"database"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "invalid request body"})
		return
	}
	if req.Query == "" {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "query is required"})
		return
	}

	pool, err := h.poolMgr.GetPool(r.Context(), req.Database)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: "failed to connect to database: " + err.Error()})
		return
	}

	start := time.Now()
	rows, err := pool.Query(r.Context(), req.Query)
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
	var results []map[string]any
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			continue
		}
		row := make(map[string]any)
		for i, col := range columns {
			if i < len(values) {
				row[col] = values[i]
			}
		}
		results = append(results, row)
	}

	if results == nil {
		results = []map[string]any{}
	}

	type SQLResult struct {
		Columns  []string         `json:"columns"`
		Rows     []map[string]any `json:"rows"`
		RowCount int              `json:"rowCount"`
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
		// Monitoring
		{Name: "Active Queries", Category: "monitoring", Query: "SELECT pid, usename, datname, state, query, now() - query_start AS duration FROM pg_stat_activity WHERE state = 'active' AND pid != pg_backend_pid() ORDER BY duration DESC;"},
		{Name: "Long Running Queries", Category: "monitoring", Query: "SELECT pid, usename, datname, state, now() - query_start AS duration, query FROM pg_stat_activity WHERE state != 'idle' AND pid != pg_backend_pid() AND now() - query_start > interval '5 seconds' ORDER BY duration DESC;"},
		{Name: "Blocking Queries", Category: "monitoring", Query: "SELECT blocked.pid AS blocked_pid, blocked.query AS blocked_query, blocking.pid AS blocking_pid, blocking.query AS blocking_query, now() - blocked.query_start AS blocked_duration FROM pg_stat_activity blocked JOIN pg_locks bl ON bl.pid = blocked.pid JOIN pg_locks bk ON bk.pid != blocked.pid AND bk.locktype = bl.locktype AND bk.database IS NOT DISTINCT FROM bl.database AND bk.relation IS NOT DISTINCT FROM bl.relation AND bk.page IS NOT DISTINCT FROM bl.page AND bk.tuple IS NOT DISTINCT FROM bl.tuple AND bk.granted JOIN pg_stat_activity blocking ON blocking.pid = bk.pid WHERE NOT bl.granted ORDER BY blocked_duration DESC;"},
		{Name: "Connection Count", Category: "monitoring", Query: "SELECT datname, usename, state, count(*) FROM pg_stat_activity GROUP BY datname, usename, state ORDER BY datname, count DESC;"},
		{Name: "Wait Events", Category: "monitoring", Query: "SELECT pid, usename, datname, wait_event_type, wait_event, state, query FROM pg_stat_activity WHERE wait_event IS NOT NULL AND pid != pg_backend_pid() ORDER BY wait_event_type;"},
		{Name: "Backend Types", Category: "monitoring", Query: "SELECT backend_type, count(*) FROM pg_stat_activity GROUP BY backend_type ORDER BY count DESC;"},
		{Name: "Lock Info", Category: "monitoring", Query: "SELECT l.pid, l.locktype, l.mode, l.granted, d.datname, c.relname FROM pg_locks l LEFT JOIN pg_database d ON l.database = d.oid LEFT JOIN pg_class c ON l.relation = c.oid WHERE l.pid != pg_backend_pid() ORDER BY l.pid;"},
		{Name: "Replication Status", Category: "monitoring", Query: "SELECT client_addr, state, sent_lsn, write_lsn, flush_lsn, replay_lsn, replay_lag FROM pg_stat_replication;"},
		{Name: "Replication Slots", Category: "monitoring", Query: "SELECT slot_name, slot_type, active, restart_lsn, confirmed_flush_lsn, pg_size_pretty(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)) AS lag_size FROM pg_replication_slots;"},

		// Performance
		{Name: "Cache Hit Ratio", Category: "performance", Query: "SELECT datname, round(blks_hit * 100.0 / NULLIF(blks_hit + blks_read, 0), 2) AS cache_hit_ratio FROM pg_stat_database WHERE datname NOT LIKE 'template%' ORDER BY datname;"},
		{Name: "Index Usage", Category: "performance", Query: "SELECT schemaname, relname, indexrelname, idx_scan, idx_tup_read, idx_tup_fetch FROM pg_stat_user_indexes ORDER BY idx_scan DESC LIMIT 20;"},
		{Name: "Unused Indexes", Category: "performance", Query: "SELECT schemaname, relname, indexrelname, pg_size_pretty(pg_relation_size(indexrelid)) AS size FROM pg_stat_user_indexes WHERE idx_scan = 0 ORDER BY pg_relation_size(indexrelid) DESC;"},
		{Name: "Sequential Scans", Category: "performance", Query: "SELECT schemaname, relname, seq_scan, seq_tup_read, idx_scan, CASE WHEN seq_scan > 0 THEN seq_tup_read / seq_scan END AS avg_tup_per_scan FROM pg_stat_user_tables WHERE seq_scan > 0 ORDER BY seq_tup_read DESC LIMIT 20;"},
		{Name: "Index Cache Hit Ratio", Category: "performance", Query: "SELECT indexrelname, round(idx_blks_hit * 100.0 / NULLIF(idx_blks_hit + idx_blks_read, 0), 2) AS hit_ratio, idx_blks_hit, idx_blks_read FROM pg_statio_user_indexes WHERE idx_blks_hit + idx_blks_read > 0 ORDER BY hit_ratio ASC LIMIT 20;"},
		{Name: "Table I/O Stats", Category: "performance", Query: "SELECT schemaname, relname, heap_blks_read, heap_blks_hit, round(heap_blks_hit * 100.0 / NULLIF(heap_blks_hit + heap_blks_read, 0), 2) AS hit_ratio FROM pg_statio_user_tables WHERE heap_blks_hit + heap_blks_read > 0 ORDER BY heap_blks_read DESC LIMIT 20;"},
		{Name: "Temp File Usage", Category: "performance", Query: "SELECT datname, temp_files, pg_size_pretty(temp_bytes) AS temp_size FROM pg_stat_database WHERE temp_files > 0 ORDER BY temp_bytes DESC;"},

		// Storage
		{Name: "Table Sizes", Category: "storage", Query: "SELECT schemaname, relname, pg_size_pretty(pg_total_relation_size(quote_ident(schemaname)||'.'||quote_ident(relname))) AS total, pg_size_pretty(pg_relation_size(quote_ident(schemaname)||'.'||quote_ident(relname))) AS table, pg_size_pretty(pg_indexes_size(quote_ident(schemaname)||'.'||quote_ident(relname))) AS indexes, n_live_tup AS rows FROM pg_stat_user_tables ORDER BY pg_total_relation_size(quote_ident(schemaname)||'.'||quote_ident(relname)) DESC LIMIT 30;"},
		{Name: "Database Sizes", Category: "storage", Query: "SELECT datname, pg_size_pretty(pg_database_size(datname)) AS size FROM pg_database WHERE datistemplate = false ORDER BY pg_database_size(datname) DESC;"},
		{Name: "Tablespace Usage", Category: "storage", Query: "SELECT spcname, pg_size_pretty(pg_tablespace_size(spcname)) AS size FROM pg_tablespace ORDER BY pg_tablespace_size(spcname) DESC;"},
		{Name: "TOAST Sizes", Category: "storage", Query: "SELECT c.relname AS table_name, pg_size_pretty(pg_total_relation_size(c.oid)) AS total, pg_size_pretty(pg_relation_size(c.oid)) AS main, pg_size_pretty(pg_total_relation_size(c.reltoastrelid)) AS toast FROM pg_class c WHERE c.relkind = 'r' AND c.reltoastrelid != 0 ORDER BY pg_total_relation_size(c.reltoastrelid) DESC LIMIT 20;"},
		{Name: "WAL Size", Category: "storage", Query: "SELECT count(*) AS wal_files, pg_size_pretty(sum(size)) AS total_size FROM pg_ls_waldir();"},

		// Maintenance
		{Name: "Vacuum Stats", Category: "maintenance", Query: "SELECT schemaname, relname, last_vacuum, last_autovacuum, last_analyze, last_autoanalyze, vacuum_count, autovacuum_count FROM pg_stat_user_tables ORDER BY COALESCE(last_autovacuum, '1970-01-01') ASC LIMIT 20;"},
		{Name: "Bloated Tables", Category: "maintenance", Query: "SELECT schemaname, relname, n_dead_tup, n_live_tup, round(n_dead_tup * 100.0 / NULLIF(n_live_tup + n_dead_tup, 0), 2) AS dead_pct FROM pg_stat_user_tables WHERE n_dead_tup > 0 ORDER BY n_dead_tup DESC LIMIT 20;"},
		{Name: "Reindex Candidates", Category: "maintenance", Query: "SELECT schemaname, tablename, indexname, pg_size_pretty(pg_relation_size(quote_ident(schemaname)||'.'||quote_ident(indexname))) AS index_size FROM pg_indexes JOIN pg_stat_user_indexes USING (schemaname, indexrelname) WHERE idx_scan = 0 ORDER BY pg_relation_size(quote_ident(schemaname)||'.'||quote_ident(indexname)) DESC LIMIT 20;"},
		{Name: "Tables Need Analyze", Category: "maintenance", Query: "SELECT schemaname, relname, last_analyze, last_autoanalyze, n_mod_since_analyze FROM pg_stat_user_tables WHERE n_mod_since_analyze > 0 ORDER BY n_mod_since_analyze DESC LIMIT 20;"},
		{Name: "Autovacuum Running", Category: "maintenance", Query: "SELECT pid, datname, relid::regclass AS table_name, phase, heap_blks_total, heap_blks_scanned, heap_blks_vacuumed FROM pg_stat_progress_vacuum;"},

		// Info
		{Name: "Extensions", Category: "info", Query: "SELECT extname, extversion, n.nspname AS schema FROM pg_extension e JOIN pg_namespace n ON e.extnamespace = n.oid ORDER BY extname;"},
		{Name: "All Settings (non-default)", Category: "info", Query: "SELECT name, setting, unit, source FROM pg_settings WHERE source != 'default' ORDER BY source, name;"},
		{Name: "Roles & Permissions", Category: "info", Query: "SELECT rolname, rolsuper, rolcreaterole, rolcreatedb, rolcanlogin, rolreplication, rolconnlimit, rolvaliduntil FROM pg_roles ORDER BY rolname;"},
		{Name: "Schema List", Category: "info", Query: "SELECT schema_name, schema_owner FROM information_schema.schemata WHERE schema_name NOT IN ('pg_catalog', 'information_schema', 'pg_toast') ORDER BY schema_name;"},
		{Name: "Foreign Keys", Category: "info", Query: "SELECT tc.table_schema, tc.table_name, kcu.column_name, ccu.table_schema AS ref_schema, ccu.table_name AS ref_table, ccu.column_name AS ref_column FROM information_schema.table_constraints tc JOIN information_schema.key_column_usage kcu ON tc.constraint_name = kcu.constraint_name JOIN information_schema.constraint_column_usage ccu ON ccu.constraint_name = tc.constraint_name WHERE tc.constraint_type = 'FOREIGN KEY' ORDER BY tc.table_schema, tc.table_name;"},
		{Name: "Triggers", Category: "info", Query: "SELECT trigger_schema, trigger_name, event_manipulation, event_object_table, action_statement FROM information_schema.triggers WHERE trigger_schema NOT IN ('pg_catalog', 'information_schema') ORDER BY trigger_schema, event_object_table;"},
		{Name: "Columns Detail", Category: "info", Query: "SELECT table_schema, table_name, column_name, data_type, is_nullable, column_default FROM information_schema.columns WHERE table_schema NOT IN ('pg_catalog', 'information_schema') ORDER BY table_schema, table_name, ordinal_position;"},
		{Name: "Row Counts (all tables)", Category: "info", Query: "SELECT schemaname, relname, n_live_tup AS estimated_rows FROM pg_stat_user_tables ORDER BY n_live_tup DESC;"},
		{Name: "Table Privileges", Category: "info", Query: "SELECT grantee, table_schema, table_name, privilege_type FROM information_schema.table_privileges WHERE table_schema NOT IN ('pg_catalog', 'information_schema') ORDER BY table_schema, table_name, grantee;"},
		{Name: "Check Constraints", Category: "info", Query: "SELECT tc.table_schema, tc.table_name, tc.constraint_name, cc.check_clause FROM information_schema.table_constraints tc JOIN information_schema.check_constraints cc ON tc.constraint_name = cc.constraint_name WHERE tc.constraint_type = 'CHECK' AND tc.table_schema NOT IN ('pg_catalog', 'information_schema') ORDER BY tc.table_schema, tc.table_name;"},
		{Name: "Enum Types", Category: "info", Query: "SELECT t.typname AS enum_name, array_agg(e.enumlabel ORDER BY e.enumsortorder) AS values FROM pg_type t JOIN pg_enum e ON t.oid = e.enumtypid GROUP BY t.typname ORDER BY t.typname;"},
		{Name: "Functions & Procedures", Category: "info", Query: "SELECT n.nspname AS schema, p.proname AS name, pg_get_function_arguments(p.oid) AS args, CASE p.prokind WHEN 'f' THEN 'function' WHEN 'p' THEN 'procedure' WHEN 'a' THEN 'aggregate' WHEN 'w' THEN 'window' END AS kind FROM pg_proc p JOIN pg_namespace n ON p.pronamespace = n.oid WHERE n.nspname NOT IN ('pg_catalog', 'information_schema') ORDER BY n.nspname, p.proname;"},

		// DDL Templates
		{Name: "CREATE TABLE", Category: "ddl", Query: "CREATE TABLE IF NOT EXISTS my_table (\n    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,\n    name VARCHAR(255) NOT NULL,\n    email VARCHAR(255) UNIQUE,\n    status VARCHAR(20) DEFAULT 'active',\n    metadata JSONB DEFAULT '{}',\n    created_at TIMESTAMPTZ DEFAULT now(),\n    updated_at TIMESTAMPTZ DEFAULT now()\n);"},
		{Name: "ALTER TABLE — Add Column", Category: "ddl", Query: "ALTER TABLE my_table\n    ADD COLUMN description TEXT,\n    ADD COLUMN priority INTEGER DEFAULT 0;"},
		{Name: "ALTER TABLE — Modify Column", Category: "ddl", Query: "ALTER TABLE my_table\n    ALTER COLUMN name SET NOT NULL,\n    ALTER COLUMN status SET DEFAULT 'pending',\n    ALTER COLUMN status TYPE VARCHAR(50);"},
		{Name: "ALTER TABLE — Drop Column", Category: "ddl", Query: "ALTER TABLE my_table\n    DROP COLUMN IF EXISTS old_column;"},
		{Name: "ALTER TABLE — Add Constraints", Category: "ddl", Query: "ALTER TABLE my_table\n    ADD CONSTRAINT fk_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,\n    ADD CONSTRAINT chk_status CHECK (status IN ('active', 'inactive', 'pending'));"},
		{Name: "CREATE INDEX", Category: "ddl", Query: "-- B-tree index\nCREATE INDEX CONCURRENTLY IF NOT EXISTS idx_my_table_name ON my_table (name);\n\n-- Partial index\nCREATE INDEX CONCURRENTLY IF NOT EXISTS idx_my_table_active ON my_table (created_at) WHERE status = 'active';\n\n-- GIN index for JSONB\nCREATE INDEX CONCURRENTLY IF NOT EXISTS idx_my_table_metadata ON my_table USING GIN (metadata);"},
		{Name: "CREATE VIEW", Category: "ddl", Query: "CREATE OR REPLACE VIEW my_view AS\nSELECT\n    t.id,\n    t.name,\n    t.status,\n    t.created_at\nFROM my_table t\nWHERE t.status = 'active'\nORDER BY t.created_at DESC;"},
		{Name: "CREATE FUNCTION", Category: "ddl", Query: "CREATE OR REPLACE FUNCTION update_updated_at()\nRETURNS TRIGGER AS $$\nBEGIN\n    NEW.updated_at = now();\n    RETURN NEW;\nEND;\n$$ LANGUAGE plpgsql;\n\nCREATE TRIGGER trg_updated_at\n    BEFORE UPDATE ON my_table\n    FOR EACH ROW\n    EXECUTE FUNCTION update_updated_at();"},
		{Name: "CREATE ENUM", Category: "ddl", Query: "CREATE TYPE status_enum AS ENUM ('active', 'inactive', 'pending', 'archived');\n\n-- Add value to existing enum\n-- ALTER TYPE status_enum ADD VALUE 'suspended' AFTER 'inactive';"},
		{Name: "CREATE EXTENSION", Category: "ddl", Query: "CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\";\nCREATE EXTENSION IF NOT EXISTS pg_trgm;\nCREATE EXTENSION IF NOT EXISTS pg_stat_statements;"},
		{Name: "DROP TABLE / Index", Category: "ddl", Query: "-- Drop table\nDROP TABLE IF EXISTS my_table CASCADE;\n\n-- Drop index\nDROP INDEX CONCURRENTLY IF EXISTS idx_my_table_name;"},
		{Name: "CREATE TABLE — Partitioned", Category: "ddl", Query: "CREATE TABLE events (\n    id BIGINT GENERATED ALWAYS AS IDENTITY,\n    event_type VARCHAR(50) NOT NULL,\n    payload JSONB,\n    created_at TIMESTAMPTZ DEFAULT now()\n) PARTITION BY RANGE (created_at);\n\nCREATE TABLE events_2025_01 PARTITION OF events\n    FOR VALUES FROM ('2025-01-01') TO ('2025-02-01');\nCREATE TABLE events_2025_02 PARTITION OF events\n    FOR VALUES FROM ('2025-02-01') TO ('2025-03-01');"},

		// GRANT / REVOKE
		{Name: "GRANT — Table", Category: "grant/revoke", Query: "-- Grant specific permissions on a table\nGRANT SELECT, INSERT, UPDATE ON my_table TO my_role;\n\n-- Grant all permissions\nGRANT ALL PRIVILEGES ON my_table TO my_role;"},
		{Name: "GRANT — Schema", Category: "grant/revoke", Query: "-- Allow role to use objects in schema\nGRANT USAGE ON SCHEMA public TO my_role;\n\n-- Allow role to create objects in schema\nGRANT CREATE ON SCHEMA public TO my_role;"},
		{Name: "GRANT — Database", Category: "grant/revoke", Query: "-- Connect permission\nGRANT CONNECT ON DATABASE my_database TO my_role;\n\n-- Create schema permission\nGRANT CREATE ON DATABASE my_database TO my_role;"},
		{Name: "GRANT — All Tables in Schema", Category: "grant/revoke", Query: "-- Grant on all existing tables\nGRANT SELECT ON ALL TABLES IN SCHEMA public TO readonly_role;\n\n-- Auto-grant on future tables\nALTER DEFAULT PRIVILEGES IN SCHEMA public\n    GRANT SELECT ON TABLES TO readonly_role;"},
		{Name: "GRANT — Column Level", Category: "grant/revoke", Query: "-- Grant SELECT on specific columns only\nGRANT SELECT (id, name, email) ON my_table TO restricted_role;\n\n-- Grant UPDATE on specific columns only\nGRANT UPDATE (status, updated_at) ON my_table TO restricted_role;"},
		{Name: "REVOKE — Remove Access", Category: "grant/revoke", Query: "-- Revoke specific permissions\nREVOKE INSERT, UPDATE, DELETE ON my_table FROM my_role;\n\n-- Revoke all permissions\nREVOKE ALL PRIVILEGES ON my_table FROM my_role;\n\n-- Revoke from all future tables\nALTER DEFAULT PRIVILEGES IN SCHEMA public\n    REVOKE ALL ON TABLES FROM my_role;"},
		{Name: "CREATE ROLE", Category: "grant/revoke", Query: "-- Read-only role\nCREATE ROLE readonly_role NOLOGIN;\nGRANT CONNECT ON DATABASE my_database TO readonly_role;\nGRANT USAGE ON SCHEMA public TO readonly_role;\nGRANT SELECT ON ALL TABLES IN SCHEMA public TO readonly_role;\n\n-- Login user with role\nCREATE ROLE app_user LOGIN PASSWORD 'secure_password';\nGRANT readonly_role TO app_user;"},
	}

	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: snippets})
}
