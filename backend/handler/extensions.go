package handler

import (
	"encoding/json"
	"net/http"

	"pgaio/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ExtensionsHandler struct {
	pool *pgxpool.Pool
}

func NewExtensionsHandler(pool *pgxpool.Pool) *ExtensionsHandler {
	return &ExtensionsHandler{pool: pool}
}

// ListExtensions returns available and installed extensions.
func (h *ExtensionsHandler) ListExtensions(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
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
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "name required"})
		return
	}
	_, err := h.pool.Exec(r.Context(), "CREATE EXTENSION IF NOT EXISTS "+req.Name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: "extension installed"})
}

// UninstallExtension drops an extension.
func (h *ExtensionsHandler) UninstallExtension(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "name required"})
		return
	}
	_, err := h.pool.Exec(r.Context(), "DROP EXTENSION IF EXISTS "+req.Name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: "extension removed"})
}
