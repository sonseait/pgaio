package handler

import (
	"encoding/json"
	"net/http"

	"pgaio/model"
	"pgaio/service"
)

type BackupHandler struct {
	walg *service.WalG
}

func NewBackupHandler(walg *service.WalG) *BackupHandler {
	return &BackupHandler{walg: walg}
}

// ListBackups returns the list of WAL-G backups.
func (h *BackupHandler) ListBackups(w http.ResponseWriter, r *http.Request) {
	backups, err := h.walg.ListBackups(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: backups})
}

// TriggerBackup starts a manual WAL-G backup.
func (h *BackupHandler) TriggerBackup(w http.ResponseWriter, r *http.Request) {
	resp, err := h.walg.TriggerBackup(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, model.APIResponse{Success: true, Data: resp})
}

// RestoreBackup handles restore requests.
func (h *BackupHandler) RestoreBackup(w http.ResponseWriter, r *http.Request) {
	var req model.RestoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "invalid request body"})
		return
	}

	resp, err := h.walg.RestoreBackup(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: resp})
}
