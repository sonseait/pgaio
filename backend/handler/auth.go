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

// GetSetup returns the TOTP setup info (secret + otpauth URL for QR).
func (h *AuthHandler) GetSetup(w http.ResponseWriter, r *http.Request) {
	info := h.totp.GetSetupInfo()
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: info})
}

// Verify validates a TOTP code (for testing the setup).
func (h *AuthHandler) Verify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "invalid request"})
		return
	}

	if h.totp.Validate(req.Code) {
		writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: map[string]string{"status": "valid"}})
	} else {
		writeJSON(w, http.StatusUnauthorized, model.APIResponse{Error: "invalid TOTP code"})
	}
}

// TOTPMiddleware wraps an http.HandlerFunc and requires a valid TOTP code.
func TOTPMiddleware(totp *service.TOTP, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.Header.Get("X-TOTP-Code")
		if code == "" {
			writeJSON(w, http.StatusUnauthorized, model.APIResponse{Error: "TOTP code required (X-TOTP-Code header)"})
			return
		}
		if !totp.Validate(code) {
			writeJSON(w, http.StatusUnauthorized, model.APIResponse{Error: "invalid TOTP code"})
			return
		}
		next(w, r)
	}
}
