package handler

import (
	"fmt"
	"net/http"

	"pgaio/model"
	"pgaio/service"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ServerOverviewHandler struct {
	poolMgr *service.PoolManager
}

func NewServerOverviewHandler(poolMgr *service.PoolManager) *ServerOverviewHandler {
	return &ServerOverviewHandler{poolMgr: poolMgr}
}

func (h *ServerOverviewHandler) getPool(r *http.Request) *pgxpool.Pool {
	db := r.URL.Query().Get("database")
	pool, err := h.poolMgr.GetPool(r.Context(), db)
	if err != nil {
		return h.poolMgr.DefaultPool()
	}
	return pool
}

// GetOverview returns database, schema, and table information.
func (h *ServerOverviewHandler) GetOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	defaultPool := h.poolMgr.DefaultPool()

	// Server info (server-global queries use default pool)
	var version string
	var startTime string
	defaultPool.QueryRow(ctx, "SELECT version()").Scan(&version)
	defaultPool.QueryRow(ctx, "SELECT pg_postmaster_start_time()::text").Scan(&startTime)

	// List databases
	type TableInfo struct {
		Schema     string `json:"schema"`
		Name       string `json:"name"`
		Rows       int64  `json:"rows"`
		Size       string `json:"size"`
		SizeBytes  int64  `json:"sizeBytes"`
		TotalBytes int64  `json:"totalBytes"`
		TableBytes int64  `json:"tableBytes"`
		IndexBytes int64  `json:"indexBytes"`
		ToastBytes int64  `json:"toastBytes"`
	}

	type SchemaInfo struct {
		Name       string      `json:"name"`
		Tables     []TableInfo `json:"tables"`
		TableCount int         `json:"tableCount"`
		Size       string      `json:"size"`
	}

	type DatabaseInfo struct {
		Name       string       `json:"name"`
		Size       string       `json:"size"`
		SizeBytes  int64        `json:"sizeBytes"`
		Owner      string       `json:"owner"`
		Encoding   string       `json:"encoding"`
		Schemas    []SchemaInfo `json:"schemas"`
		TableCount int          `json:"tableCount"`
	}

	type Overview struct {
		Version     string         `json:"version"`
		StartTime   string         `json:"startTime"`
		Databases   []DatabaseInfo `json:"databases"`
		TotalDBs    int            `json:"totalDbs"`
		TotalTables int            `json:"totalTables"`
	}

	// Get databases (server-global query)
	dbRows, err := defaultPool.Query(ctx, `
		SELECT d.datname, pg_database_size(d.datname)::text, pg_database_size(d.datname),
			   r.rolname, pg_encoding_to_char(d.encoding)
		FROM pg_database d
		JOIN pg_roles r ON d.datdba = r.oid
		WHERE d.datistemplate = false
		ORDER BY d.datname
	`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	defer dbRows.Close()

	var databases []DatabaseInfo
	totalTables := 0
	for dbRows.Next() {
		var db DatabaseInfo
		var sizeRaw string
		if err := dbRows.Scan(&db.Name, &sizeRaw, &db.SizeBytes, &db.Owner, &db.Encoding); err != nil {
			continue
		}
		db.Size = formatBytesGo(db.SizeBytes)
		databases = append(databases, db)
	}

	// Get schemas and tables for each database using pool manager
	for i, db := range databases {
		dbPool, err := h.poolMgr.GetPool(ctx, db.Name)
		if err != nil {
			continue
		}

		// Get schemas
		schemaRows, err := dbPool.Query(ctx, `
			SELECT schema_name FROM information_schema.schemata
			WHERE schema_name NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
			ORDER BY schema_name
		`)
		if err != nil {
			continue
		}

		schemaMap := make(map[string]*SchemaInfo)
		var schemas []SchemaInfo
		for schemaRows.Next() {
			var s SchemaInfo
			schemaRows.Scan(&s.Name)
			schemas = append(schemas, s)
			schemaMap[s.Name] = &schemas[len(schemas)-1]
		}
		schemaRows.Close()

		// Get tables with sizes
		tableRows, err := dbPool.Query(ctx, `
			SELECT schemaname, relname,
				   n_live_tup,
				   pg_size_pretty(pg_total_relation_size(quote_ident(schemaname)||'.'||quote_ident(relname))),
				   pg_total_relation_size(quote_ident(schemaname)||'.'||quote_ident(relname)),
				   pg_relation_size(quote_ident(schemaname)||'.'||quote_ident(relname)),
				   pg_indexes_size(quote_ident(schemaname)||'.'||quote_ident(relname)),
				   GREATEST(pg_total_relation_size(quote_ident(schemaname)||'.'||quote_ident(relname)) -
				     pg_relation_size(quote_ident(schemaname)||'.'||quote_ident(relname)) -
				     pg_indexes_size(quote_ident(schemaname)||'.'||quote_ident(relname)), 0)
			FROM pg_stat_user_tables
			ORDER BY pg_total_relation_size(quote_ident(schemaname)||'.'||quote_ident(relname)) DESC
		`)
		if err == nil {
			dbTableCount := 0
			for tableRows.Next() {
				var t TableInfo
				var schema string
				tableRows.Scan(&schema, &t.Name, &t.Rows, &t.Size, &t.SizeBytes, &t.TableBytes, &t.IndexBytes, &t.ToastBytes)
				t.TotalBytes = t.SizeBytes
				t.Schema = schema
				if si, ok := schemaMap[schema]; ok {
					si.Tables = append(si.Tables, t)
					si.TableCount++
				}
				dbTableCount++
			}
			tableRows.Close()
			databases[i].TableCount = dbTableCount
			totalTables += dbTableCount
		}

		// Calculate schema sizes
		for j := range schemas {
			var totalSize int64
			for _, t := range schemas[j].Tables {
				totalSize += t.SizeBytes
			}
			schemas[j].Size = formatBytesGo(totalSize)
		}

		databases[i].Schemas = schemas
	}

	overview := Overview{
		Version:     version,
		StartTime:   startTime,
		Databases:   databases,
		TotalDBs:    len(databases),
		TotalTables: totalTables,
	}

	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: overview})
}

func formatBytesGo(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	exp := 0
	val := float64(b)
	for val >= unit && exp < 4 {
		val /= unit
		exp++
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	return fmt.Sprintf("%.1f %s", val, units[exp])
}
