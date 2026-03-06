package handler

import (
	"encoding/json"
	"net/http"

	"pgaio/model"
	"pgaio/service"
)

type SettingsHandler struct {
	config    *service.ConfigStore
	scheduler interface {
		Restart()
		Status() map[string]interface{}
	}
}

func NewSettingsHandler(config *service.ConfigStore, scheduler interface {
	Restart()
	Status() map[string]interface{}
}) *SettingsHandler {
	return &SettingsHandler{config: config, scheduler: scheduler}
}

// GetSettings returns current app config.
func (h *SettingsHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	cfg := h.config.Get()
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: cfg})
}

// UpdateSettings saves new app config.
func (h *SettingsHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var cfg service.AppConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "invalid config: " + err.Error()})
		return
	}
	if err := h.config.Update(cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	// Restart scheduler with new config
	if h.scheduler != nil {
		h.scheduler.Restart()
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: "settings updated"})
}

// GetScheduleStatus returns backup scheduler status.
func (h *SettingsHandler) GetScheduleStatus(w http.ResponseWriter, r *http.Request) {
	if h.scheduler == nil {
		writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: map[string]interface{}{"enabled": false}})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: h.scheduler.Status()})
}
