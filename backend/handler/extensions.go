package handler

import (
	"encoding/json"
	"net/http"

	"pgaio/model"
	"pgaio/service"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ExtensionsHandler struct {
	poolMgr *service.PoolManager
}

func NewExtensionsHandler(poolMgr *service.PoolManager) *ExtensionsHandler {
	return &ExtensionsHandler{poolMgr: poolMgr}
}

func (h *ExtensionsHandler) getPool(r *http.Request) *pgxpool.Pool {
	db := r.URL.Query().Get("database")
	pool, err := h.poolMgr.GetPool(r.Context(), db)
	if err != nil {
		return h.poolMgr.DefaultPool()
	}
	return pool
}

// ListExtensions returns available and installed extensions.
func (h *ExtensionsHandler) ListExtensions(w http.ResponseWriter, r *http.Request) {
	pool := h.getPool(r)
	rows, err := pool.Query(r.Context(), `
		SELECT a.name, a.default_version, a.comment,
		       e.extversion as installed_version
		FROM pg_available_extensions a
		LEFT JOIN pg_extension e ON e.extname = a.name
		ORDER BY a.name
	`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	defer rows.Close()

	type Extension struct {
		Name             string  `json:"name"`
		DefaultVersion   string  `json:"defaultVersion"`
		Comment          *string `json:"comment"`
		InstalledVersion *string `json:"installedVersion"`
		Installed        bool    `json:"installed"`
	}

	var exts []Extension
	for rows.Next() {
		var ext Extension
		if err := rows.Scan(&ext.Name, &ext.DefaultVersion, &ext.Comment, &ext.InstalledVersion); err != nil {
			continue
		}
		ext.Installed = ext.InstalledVersion != nil
		exts = append(exts, ext)
	}

	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: exts})
}

// InstallExtension creates an extension.
func (h *ExtensionsHandler) InstallExtension(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Database string `json:"database"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "name required"})
		return
	}
	pool, err := h.poolMgr.GetPool(r.Context(), req.Database)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: "failed to connect to database: " + err.Error()})
		return
	}
	_, err = pool.Exec(r.Context(), "CREATE EXTENSION IF NOT EXISTS "+req.Name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: "extension installed"})
}

// UninstallExtension drops an extension.
func (h *ExtensionsHandler) UninstallExtension(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Database string `json:"database"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "name required"})
		return
	}
	pool, err := h.poolMgr.GetPool(r.Context(), req.Database)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: "failed to connect to database: " + err.Error()})
		return
	}
	_, err = pool.Exec(r.Context(), "DROP EXTENSION IF EXISTS "+req.Name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: "extension removed"})
}
