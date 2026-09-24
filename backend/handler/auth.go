package handler

import (
	"encoding/json"
	"net/http"

	"pgaio/model"
	"pgaio/service"
)

type AuthHandler struct {
	auth *service.PasswordAuth
}

func NewAuthHandler(auth *service.PasswordAuth) *AuthHandler {
	return &AuthHandler{auth: auth}
}

// Login validates the dashboard password and creates a new session.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "invalid request"})
		return
	}
	sessionID, err := h.auth.Login(req.Password)
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
func SessionMiddleware(auth *service.PasswordAuth, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.Header.Get("X-Session-ID")
		if sessionID == "" {
			sessionID = r.URL.Query().Get("session_id")
		}
		if sessionID == "" {
			writeJSON(w, http.StatusUnauthorized, model.APIResponse{Error: "session required"})
			return
		}
		if !auth.ValidateSession(sessionID) {
			writeJSON(w, http.StatusUnauthorized, model.APIResponse{Error: "session expired"})
			return
		}
		next(w, r)
	}
}
