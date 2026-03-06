package handler

import (
	"context"
	"net/http"

	"pgaio/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ConfigHandler struct {
	pool *pgxpool.Pool
}

func NewConfigHandler(pool *pgxpool.Pool) *ConfigHandler {
	return &ConfigHandler{pool: pool}
}

// GetConfig returns PostgreSQL configuration settings.
func (h *ConfigHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT name, setting, unit, category, short_desc, source, boot_val, reset_val,
			   vartype, min_val, max_val, context
		FROM pg_settings
		ORDER BY category, name
	`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	defer rows.Close()

	type ConfigItem struct {
		Name     string  `json:"name"`
		Setting  string  `json:"setting"`
		Unit     *string `json:"unit"`
		Category string  `json:"category"`
		Desc     string  `json:"desc"`
		Source   string  `json:"source"`
		BootVal  *string `json:"bootVal"`
		ResetVal *string `json:"resetVal"`
		VarType  string  `json:"varType"`
		MinVal   *string `json:"minVal"`
		MaxVal   *string `json:"maxVal"`
		Context  string  `json:"context"`
	}

	var items []ConfigItem
	for rows.Next() {
		var item ConfigItem
		if err := rows.Scan(&item.Name, &item.Setting, &item.Unit, &item.Category,
			&item.Desc, &item.Source, &item.BootVal, &item.ResetVal,
			&item.VarType, &item.MinVal, &item.MaxVal, &item.Context); err != nil {
			continue
		}
		items = append(items, item)
	}

	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: items})
}

// SetConfig applies a PostgreSQL configuration change.
func (h *ConfigHandler) SetConfig(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	value := r.URL.Query().Get("value")
	if name == "" || value == "" {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "name and value required"})
		return
	}

	// Use ALTER SYSTEM for persistent changes
	_, err := h.pool.Exec(context.Background(), "ALTER SYSTEM SET "+name+" = '"+value+"'")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}

	// Reload configuration
	_, err = h.pool.Exec(context.Background(), "SELECT pg_reload_conf()")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: "setting changed but reload failed: " + err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: "configuration updated"})
}
