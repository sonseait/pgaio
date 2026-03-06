package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"pgaio/model"
	"pgaio/service"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

type DashboardHandler struct {
	monitor *service.Monitor
}

func NewDashboardHandler(monitor *service.Monitor) *DashboardHandler {
	return &DashboardHandler{monitor: monitor}
}

// GetStats returns current PostgreSQL statistics.
func (h *DashboardHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.monitor.CollectStats(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: stats})
}

// StreamStats streams stats via WebSocket.
func (h *DashboardHandler) StreamStats(w http.ResponseWriter, r *http.Request) {
	intervalStr := r.URL.Query().Get("interval")
	interval := 3 * time.Second
	if intervalStr != "" {
		if v, err := strconv.Atoi(intervalStr); err == nil && v >= 1 && v <= 30 {
			interval = time.Duration(v) * time.Second
		}
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Allow connections from any origin
	})
	if err != nil {
		log.Printf("[ws] accept error: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	log.Printf("[ws] client connected, interval: %v", interval)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[ws] client disconnected")
			return
		case <-ticker.C:
			stats, err := h.monitor.CollectStats(ctx)
			if err != nil {
				log.Printf("[ws] collect error: %v", err)
				continue
			}
			if err := wsjson.Write(ctx, conn, stats); err != nil {
				return // client disconnected
			}
		}
	}
}

// CancelQuery cancels a query by PID.
func (h *DashboardHandler) CancelQuery(w http.ResponseWriter, r *http.Request) {
	pidStr := r.PathValue("pid")
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "invalid pid"})
		return
	}
	if err := h.monitor.CancelQuery(r.Context(), pid); err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: "query cancelled"})
}

// TerminateBackend terminates a backend by PID.
func (h *DashboardHandler) TerminateBackend(w http.ResponseWriter, r *http.Request) {
	pidStr := r.PathValue("pid")
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "invalid pid"})
		return
	}
	if err := h.monitor.TerminateBackend(r.Context(), pid); err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: "backend terminated"})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
