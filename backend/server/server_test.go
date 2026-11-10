package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"pgaio/service"
)

func TestServeHTTPProtectsAllAPIEndpointsExceptLogin(t *testing.T) {
	t.Setenv("PGAIO_PASSWORD", "test-password")
	auth, err := service.NewPasswordAuth()
	if err != nil {
		t.Fatalf("new auth: %v", err)
	}
	server := &Server{mux: http.NewServeMux(), authService: auth}
	server.mux.HandleFunc("GET /api/private", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	server.mux.HandleFunc("POST /api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/api/private", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated API response = %d, want %d", response.Code, http.StatusUnauthorized)
	}

	sessionID, err := auth.Login("test-password")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/private", nil)
	request.Header.Set("X-Session-ID", sessionID)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("authenticated API response = %d, want %d", response.Code, http.StatusNoContent)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("login response = %d, want %d", response.Code, http.StatusNoContent)
	}
}
