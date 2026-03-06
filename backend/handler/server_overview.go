package handler

import (
	"fmt"
	"net/http"

	"pgaio/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ServerOverviewHandler struct {
	pool *pgxpool.Pool
}

func NewServerOverviewHandler(pool *pgxpool.Pool) *ServerOverviewHandler {
	return &ServerOverviewHandler{pool: pool}
}

// GetOverview returns database, schema, and table information.
func (h *ServerOverviewHandler) GetOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Server info
	var version string
	var startTime string
	h.pool.QueryRow(ctx, "SELECT version()").Scan(&version)
	h.pool.QueryRow(ctx, "SELECT pg_postmaster_start_time()::text").Scan(&startTime)

	// List databases
	type TableInfo struct {
		Schema    string `json:"schema"`
		Name      string `json:"name"`
		Rows      int64  `json:"rows"`
		Size      string `json:"size"`
		SizeBytes int64  `json:"sizeBytes"`
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

	// Get databases
	dbRows, err := h.pool.Query(ctx, `
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

	// Get schemas and tables for the current database only
	for i, db := range databases {
		// We can only query the current database
		var currentDB string
		h.pool.QueryRow(ctx, "SELECT current_database()").Scan(&currentDB)
		if db.Name != currentDB {
			continue
		}

		// Get schemas
		schemaRows, err := h.pool.Query(ctx, `
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
		tableRows, err := h.pool.Query(ctx, `
			SELECT schemaname, relname,
				   n_live_tup,
				   pg_size_pretty(pg_total_relation_size(schemaname || '.' || relname)),
				   pg_total_relation_size(schemaname || '.' || relname)
			FROM pg_stat_user_tables
			ORDER BY schemaname, relname
		`)
		if err == nil {
			dbTableCount := 0
			for tableRows.Next() {
				var t TableInfo
				var schema string
				tableRows.Scan(&schema, &t.Name, &t.Rows, &t.Size, &t.SizeBytes)
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
