package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"pgaio/model"
	"pgaio/service"
)

type BackupHandler struct {
	walg *service.WalG
}

func (h *BackupHandler) VerifyBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BackupName string `json:"backupName"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	resp, err := h.walg.VerifyBackup(r.Context(), strings.TrimSpace(req.BackupName))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, model.APIResponse{Success: true, Data: resp})
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
	var req struct {
		Kind string `json:"kind"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "invalid request body"})
			return
		}
	}
	var resp *model.BackupTriggerResponse
	var err error
	switch req.Kind {
	case "full":
		resp, err = h.walg.TriggerFullBackup(r.Context())
	case "", "incremental":
		resp, err = h.walg.TriggerBackup(r.Context())
	default:
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "kind must be full or incremental"})
		return
	}
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
