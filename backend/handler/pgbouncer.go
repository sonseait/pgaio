package handler

import (
	"encoding/json"
	"net/http"

	"pgaio/model"
	"pgaio/service"
)

type PgBouncerHandler struct {
	pgb *service.PgBouncer
}

func NewPgBouncerHandler(pgb *service.PgBouncer) *PgBouncerHandler {
	return &PgBouncerHandler{pgb: pgb}
}

// GetStats returns full PgBouncer statistics.
func (h *PgBouncerHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	if h.pgb == nil {
		writeJSON(w, http.StatusServiceUnavailable, model.APIResponse{Error: "PgBouncer not configured"})
		return
	}
	stats, err := h.pgb.GetFullStats(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: stats})
}

type pgbAction struct {
	Database string `json:"database"`
}

// Pause pauses a PgBouncer pool.
func (h *PgBouncerHandler) Pause(w http.ResponseWriter, r *http.Request) {
	if h.pgb == nil {
		writeJSON(w, http.StatusServiceUnavailable, model.APIResponse{Error: "PgBouncer not configured"})
		return
	}
	var req pgbAction
	json.NewDecoder(r.Body).Decode(&req)

	if err := h.pgb.Pause(r.Context(), req.Database); err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: "paused"})
}

// Resume resumes a PgBouncer pool.
func (h *PgBouncerHandler) Resume(w http.ResponseWriter, r *http.Request) {
	if h.pgb == nil {
		writeJSON(w, http.StatusServiceUnavailable, model.APIResponse{Error: "PgBouncer not configured"})
		return
	}
	var req pgbAction
	json.NewDecoder(r.Body).Decode(&req)

	if err := h.pgb.Resume(r.Context(), req.Database); err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: "resumed"})
}

// Reload reloads PgBouncer configuration.
func (h *PgBouncerHandler) Reload(w http.ResponseWriter, r *http.Request) {
	if h.pgb == nil {
		writeJSON(w, http.StatusServiceUnavailable, model.APIResponse{Error: "PgBouncer not configured"})
		return
	}
	if err := h.pgb.Reload(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: "reloaded"})
}

// Kill kills all connections for a database.
func (h *PgBouncerHandler) Kill(w http.ResponseWriter, r *http.Request) {
	if h.pgb == nil {
		writeJSON(w, http.StatusServiceUnavailable, model.APIResponse{Error: "PgBouncer not configured"})
		return
	}
	var req pgbAction
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Database == "" {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "database name required"})
		return
	}
	if err := h.pgb.Kill(r.Context(), req.Database); err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: "killed"})
}
