package handler

import (
	"encoding/json"
	"net/http"

	"pgaio/model"
	"pgaio/service"
)

type AuthHandler struct {
	totp *service.TOTP
}

func NewAuthHandler(totp *service.TOTP) *AuthHandler {
	return &AuthHandler{totp: totp}
}

// GetStatus returns whether TOTP is set up and if the current session is valid.
func (h *AuthHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("X-Session-ID")
	writeJSON(w, http.StatusOK, model.APIResponse{
		Success: true,
		Data: map[string]any{
			"setup":   h.totp.IsSetup(),
			"session": h.totp.CheckSession(sessionID),
		},
	})
}

// GetSetup generates a pending secret for first-time TOTP setup.
// Returns 403 if TOTP is already configured.
func (h *AuthHandler) GetSetup(w http.ResponseWriter, r *http.Request) {
	if h.totp.IsSetup() {
		writeJSON(w, http.StatusForbidden, model.APIResponse{Error: "TOTP already configured"})
		return
	}
	info, err := h.totp.GeneratePendingSecret()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: info})
}

// ConfirmSetup verifies the code against pending secret, saves to file, creates session.
func (h *AuthHandler) ConfirmSetup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "invalid request"})
		return
	}
	sessionID, err := h.totp.ConfirmSetup(req.Code)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{
		Success: true,
		Data:    map[string]string{"sessionId": sessionID},
	})
}

// Login validates TOTP code and creates a new session.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "invalid request"})
		return
	}
	sessionID, err := h.totp.Login(req.Code)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{
		Success: true,
		Data:    map[string]string{"sessionId": sessionID},
	})
}

// SessionMiddleware wraps a handler and requires a valid session.
func SessionMiddleware(totp *service.TOTP, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.Header.Get("X-Session-ID")
		if sessionID == "" {
			writeJSON(w, http.StatusUnauthorized, model.APIResponse{Error: "session required"})
			return
		}
		if !totp.ValidateSession(sessionID) {
			writeJSON(w, http.StatusUnauthorized, model.APIResponse{Error: "session expired"})
			return
		}
		next(w, r)
	}
}
