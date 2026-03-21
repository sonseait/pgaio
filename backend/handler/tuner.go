package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"pgaio/model"
	"pgaio/service"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TunerHandler handles database tuning wizard API requests.
type TunerHandler struct {
	tuner     *service.Tuner
	pool      *pgxpool.Pool
	pgbouncer *service.PgBouncer
}

// NewTunerHandler creates a new TunerHandler.
func NewTunerHandler(tuner *service.Tuner, pool *pgxpool.Pool, pgbouncer *service.PgBouncer) *TunerHandler {
	return &TunerHandler{
		tuner:     tuner,
		pool:      pool,
		pgbouncer: pgbouncer,
	}
}

// GetSystemInfo returns detected system information.
func (h *TunerHandler) GetSystemInfo(w http.ResponseWriter, r *http.Request) {
	info := h.tuner.DetectSystem(r.Context())
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: info})
}

// Analyze performs a full tuning analysis.
func (h *TunerHandler) Analyze(w http.ResponseWriter, r *http.Request) {
	var req service.TuneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "invalid request: " + err.Error()})
		return
	}

	result, err := h.tuner.Analyze(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: result})
}

// ApplyRequest is the request to apply recommended config changes.
type ApplyRequest struct {
	PostgresSettings  []ApplySetting          `json:"postgresSettings"`
	PgBouncerSettings *service.PgBouncerConnectionConfig `json:"pgbouncerSettings,omitempty"`
}

// ApplySetting is a single setting to apply.
type ApplySetting struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ApplyResult is the result of applying settings.
type ApplyResult struct {
	Applied     []string `json:"applied"`
	Failed      []string `json:"failed"`
	NeedRestart bool     `json:"needRestart"`
	Message     string   `json:"message"`
}

// Apply applies selected tuning recommendations.
func (h *TunerHandler) Apply(w http.ResponseWriter, r *http.Request) {
	var req ApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "invalid request: " + err.Error()})
		return
	}

	ctx := r.Context()
	result := ApplyResult{}
	needRestart := false

	// Apply PostgreSQL settings via ALTER SYSTEM
	restartParams := map[string]bool{
		"shared_buffers":      true,
		"max_connections":     true,
		"huge_pages":          true,
		"max_worker_processes": true,
		"wal_buffers":         true,
	}

	for _, s := range req.PostgresSettings {
		_, err := h.pool.Exec(ctx, fmt.Sprintf("ALTER SYSTEM SET %s = '%s'", s.Name, s.Value))
		if err != nil {
			result.Failed = append(result.Failed, fmt.Sprintf("%s: %s", s.Name, err.Error()))
			continue
		}
		result.Applied = append(result.Applied, s.Name)
		if restartParams[s.Name] {
			needRestart = true
		}
	}

	// Reload PostgreSQL config for non-restart params
	if len(result.Applied) > 0 {
		h.pool.Exec(ctx, "SELECT pg_reload_conf()")
	}

	// Apply PgBouncer settings if provided
	if req.PgBouncerSettings != nil && h.pgbouncer != nil {
		err := h.pgbouncer.UpdateConfig(ctx, map[string]string{
			"max_client_conn":    fmt.Sprintf("%d", req.PgBouncerSettings.MaxClientConn),
			"default_pool_size":  fmt.Sprintf("%d", req.PgBouncerSettings.DefaultPoolSize),
			"min_pool_size":      fmt.Sprintf("%d", req.PgBouncerSettings.MinPoolSize),
			"reserve_pool_size":  fmt.Sprintf("%d", req.PgBouncerSettings.ReservePoolSize),
			"max_db_connections": fmt.Sprintf("%d", req.PgBouncerSettings.MaxDbConnections),
		})
		if err != nil {
			result.Failed = append(result.Failed, "pgbouncer config: "+err.Error())
		} else {
			result.Applied = append(result.Applied, "pgbouncer_config")
		}
	}

	result.NeedRestart = needRestart
	if needRestart {
		result.Message = fmt.Sprintf("%d settings applied. PostgreSQL RESTART required for some changes to take effect.", len(result.Applied))
	} else {
		result.Message = fmt.Sprintf("%d settings applied and reloaded successfully.", len(result.Applied))
	}

	if result.Applied == nil {
		result.Applied = []string{}
	}
	if result.Failed == nil {
		result.Failed = []string{}
	}

	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: result})
}
