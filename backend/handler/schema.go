package handler

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"pgaio/model"
	"pgaio/service"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SchemaHandler struct {
	poolMgr *service.PoolManager
}

func NewSchemaHandler(poolMgr *service.PoolManager) *SchemaHandler {
	return &SchemaHandler{poolMgr: poolMgr}
}

type schemaSnapshot struct {
	Tables     map[string]string
	Columns    map[string]string
	Indexes    map[string]string
	Extensions map[string]string
}

func (h *SchemaHandler) GetDrift(w http.ResponseWriter, r *http.Request) {
	sourceDB := strings.TrimSpace(r.URL.Query().Get("source"))
	targetDB := strings.TrimSpace(r.URL.Query().Get("target"))
	profile := strings.TrimSpace(r.URL.Query().Get("profile"))
	if sourceDB == "" || targetDB == "" {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "source and target databases are required"})
		return
	}

	sourcePool, err := h.poolMgr.GetPoolForProfile(r.Context(), sourceDB, profile)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: "failed to connect source database: " + err.Error()})
		return
	}
	targetPool, err := h.poolMgr.GetPoolForProfile(r.Context(), targetDB, profile)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: "failed to connect target database: " + err.Error()})
		return
	}

	source, err := loadSchemaSnapshot(r.Context(), sourcePool)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: "failed to inspect source schema: " + err.Error()})
		return
	}
	target, err := loadSchemaSnapshot(r.Context(), targetPool)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: "failed to inspect target schema: " + err.Error()})
		return
	}

	data := map[string]any{
		"source":  sourceDB,
		"target":  targetDB,
		"profile": profile,
		"summary": map[string]any{
			"tableOnlyInSource":     diffOnly(source.Tables, target.Tables),
			"tableOnlyInTarget":     diffOnly(target.Tables, source.Tables),
			"tableChanged":          diffChanged(source.Tables, target.Tables),
			"columnOnlyInSource":    diffOnly(source.Columns, target.Columns),
			"columnOnlyInTarget":    diffOnly(target.Columns, source.Columns),
			"columnChanged":         diffChanged(source.Columns, target.Columns),
			"indexOnlyInSource":     diffOnly(source.Indexes, target.Indexes),
			"indexOnlyInTarget":     diffOnly(target.Indexes, source.Indexes),
			"indexChanged":          diffChanged(source.Indexes, target.Indexes),
			"extensionOnlyInSource": diffOnly(source.Extensions, target.Extensions),
			"extensionOnlyInTarget": diffOnly(target.Extensions, source.Extensions),
			"extensionChanged":      diffChanged(source.Extensions, target.Extensions),
		},
	}

	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: data})
}

func loadSchemaSnapshot(ctx context.Context, pool *pgxpool.Pool) (*schemaSnapshot, error) {
	snapshot := &schemaSnapshot{
		Tables:     make(map[string]string),
		Columns:    make(map[string]string),
		Indexes:    make(map[string]string),
		Extensions: make(map[string]string),
	}

	tableRows, err := pool.Query(ctx, `
		SELECT n.nspname, c.relname,
		       pg_get_userbyid(c.relowner),
		       COALESCE(obj_description(c.oid, 'pg_class'), '')
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('r', 'p')
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		ORDER BY n.nspname, c.relname
	`)
	if err != nil {
		return nil, err
	}
	for tableRows.Next() {
		var schema, table, owner, comment string
		if err := tableRows.Scan(&schema, &table, &owner, &comment); err == nil {
			snapshot.Tables[schema+"."+table] = owner + "|" + comment
		}
	}
	tableRows.Close()

	columnRows, err := pool.Query(ctx, `
		SELECT table_schema, table_name, column_name,
		       data_type,
		       is_nullable,
		       COALESCE(column_default, ''),
		       COALESCE(character_maximum_length::text, '')
		FROM information_schema.columns
		WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
		ORDER BY table_schema, table_name, ordinal_position
	`)
	if err != nil {
		return nil, err
	}
	for columnRows.Next() {
		var schema, table, column, dataType, nullable, defaultValue, maxLength string
		if err := columnRows.Scan(&schema, &table, &column, &dataType, &nullable, &defaultValue, &maxLength); err == nil {
			snapshot.Columns[schema+"."+table+"."+column] = strings.Join([]string{dataType, nullable, defaultValue, maxLength}, "|")
		}
	}
	columnRows.Close()

	indexRows, err := pool.Query(ctx, `
		SELECT schemaname, tablename, indexname, indexdef
		FROM pg_indexes
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
		ORDER BY schemaname, tablename, indexname
	`)
	if err != nil {
		return nil, err
	}
	for indexRows.Next() {
		var schema, table, indexName, definition string
		if err := indexRows.Scan(&schema, &table, &indexName, &definition); err == nil {
			snapshot.Indexes[schema+"."+table+"."+indexName] = definition
		}
	}
	indexRows.Close()

	extensionRows, err := pool.Query(ctx, `
		SELECT extname, extversion
		FROM pg_extension
		ORDER BY extname
	`)
	if err != nil {
		return nil, err
	}
	for extensionRows.Next() {
		var name, version string
		if err := extensionRows.Scan(&name, &version); err == nil {
			snapshot.Extensions[name] = version
		}
	}
	extensionRows.Close()

	return snapshot, nil
}

func diffOnly(left, right map[string]string) []map[string]string {
	out := make([]map[string]string, 0)
	for key, value := range left {
		if _, ok := right[key]; ok {
			continue
		}
		out = append(out, map[string]string{
			"name":   key,
			"detail": value,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["name"] < out[j]["name"] })
	return out
}

func diffChanged(left, right map[string]string) []map[string]string {
	out := make([]map[string]string, 0)
	for key, leftValue := range left {
		rightValue, ok := right[key]
		if !ok || leftValue == rightValue {
			continue
		}
		out = append(out, map[string]string{
			"name":         key,
			"sourceDetail": leftValue,
			"targetDetail": rightValue,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["name"] < out[j]["name"] })
	return out
}
