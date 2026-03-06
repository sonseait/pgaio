package handler

import (
	"net/http"

	"pgaio/model"
	"pgaio/service"
)

type AlertsHandler struct {
	alerter *service.Alerter
}

func NewAlertsHandler(alerter *service.Alerter) *AlertsHandler {
	return &AlertsHandler{alerter: alerter}
}

// GetAlerts returns alert status and history.
func (h *AlertsHandler) GetAlerts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: h.alerter.GetStatus()})
}

// TestAlert sends a test notification.
func (h *AlertsHandler) TestAlert(w http.ResponseWriter, r *http.Request) {
	if err := h.alerter.SendTestNotification(); err != nil {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: "test notification sent"})
}
