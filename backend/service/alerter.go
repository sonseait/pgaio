package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// Alerter monitors system health and sends notifications.
type Alerter struct {
	config  *ConfigStore
	monitor *Monitor
	walg    *WalG
	history []AlertEvent
}

type AlertEvent struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"` // warning, critical
	Message string    `json:"message"`
	Metric  string    `json:"metric"`
	Value   string    `json:"value"`
}

func NewAlerter(config *ConfigStore, monitor *Monitor, walg *WalG) *Alerter {
	a := &Alerter{config: config, monitor: monitor, walg: walg}
	go a.loop()
	return a
}

func (a *Alerter) loop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		cfg := a.config.GetAlerts()
		if !cfg.Enabled {
			continue
		}
		a.check(cfg)
	}
}

func (a *Alerter) check(cfg AlertConfig) {
	stats, err := a.monitor.CollectStats(context.TODO())
	if err != nil {
		return
	}

	// Disk usage
	if cfg.Thresholds.DiskUsagePct > 0 && stats.System.DiskUsage > float64(cfg.Thresholds.DiskUsagePct) {
		a.fire(cfg, "critical", "disk_usage",
			fmt.Sprintf("Disk usage %.1f%% exceeds %d%%", stats.System.DiskUsage, cfg.Thresholds.DiskUsagePct),
			fmt.Sprintf("%.1f%%", stats.System.DiskUsage))
	}

	// Connection usage
	if cfg.Thresholds.ConnectionsPct > 0 && stats.Connections.MaxConnections > 0 {
		pct := float64(stats.Connections.UsedConnections) / float64(stats.Connections.MaxConnections) * 100
		if pct > float64(cfg.Thresholds.ConnectionsPct) {
			a.fire(cfg, "warning", "connections",
				fmt.Sprintf("Connection usage %.0f%% (%d/%d) exceeds %d%%",
					pct, stats.Connections.UsedConnections, stats.Connections.MaxConnections, cfg.Thresholds.ConnectionsPct),
				fmt.Sprintf("%.0f%%", pct))
		}
	}
}

func (a *Alerter) fire(cfg AlertConfig, level, metric, message, value string) {
	event := AlertEvent{
		Time:    time.Now(),
		Level:   level,
		Message: message,
		Metric:  metric,
		Value:   value,
	}

	// Keep last 100 events
	a.history = append(a.history, event)
	if len(a.history) > 100 {
		a.history = a.history[len(a.history)-100:]
	}

	log.Printf("🚨 ALERT [%s] %s: %s", level, metric, message)

	// Send Telegram if configured
	if cfg.Telegram.BotToken != "" && cfg.Telegram.ChatID != "" {
		go a.sendTelegram(cfg.Telegram, fmt.Sprintf("🚨 PGAIO [%s]\n%s", strings.ToUpper(level), message))
	}
}

func (a *Alerter) sendTelegram(cfg TelegramConfig, text string) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.BotToken)
	body := fmt.Sprintf(`{"chat_id":"%s","text":"%s","parse_mode":"HTML"}`, cfg.ChatID, text)
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		log.Printf("⚠️  Telegram send failed: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Printf("⚠️  Telegram API returned %d", resp.StatusCode)
	}
}

// SendTestNotification sends a test alert.
func (a *Alerter) SendTestNotification() error {
	cfg := a.config.GetAlerts()
	if cfg.Telegram.BotToken == "" || cfg.Telegram.ChatID == "" {
		return fmt.Errorf("telegram not configured")
	}
	a.sendTelegram(cfg.Telegram, "✅ PGAIO test notification — alerting is working!")
	return nil
}

// GetHistory returns alert history.
func (a *Alerter) GetHistory() []AlertEvent {
	return a.history
}

// GetStatus returns current alert status.
func (a *Alerter) GetStatus() map[string]interface{} {
	cfg := a.config.GetAlerts()
	return map[string]interface{}{
		"enabled":    cfg.Enabled,
		"thresholds": cfg.Thresholds,
		"telegram":   cfg.Telegram.BotToken != "",
		"history":    a.history,
	}
}

// MarshalJSON for AlertEvent
func init() {
	_ = json.Marshal
}
