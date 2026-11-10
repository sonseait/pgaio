package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Alerter monitors system health and sends notifications.
type Alerter struct {
	config  *ConfigStore
	monitor *Monitor
	walg    *WalG
	mu      sync.RWMutex
	history []AlertEvent
	active  map[string]*AlertState
}

type AlertEvent struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"` // warning, critical, resolved
	Message string    `json:"message"`
	Metric  string    `json:"metric"`
	Value   string    `json:"value"`
	Key     string    `json:"key,omitempty"`
	Status  string    `json:"status,omitempty"`
}

type AlertState struct {
	Key         string     `json:"key"`
	Metric      string     `json:"metric"`
	Level       string     `json:"level"`
	Status      string     `json:"status"`
	Message     string     `json:"message"`
	Value       string     `json:"value"`
	OpenedAt    time.Time  `json:"openedAt"`
	LastSeenAt  time.Time  `json:"lastSeenAt"`
	LastFiredAt time.Time  `json:"lastFiredAt"`
	ResolvedAt  *time.Time `json:"resolvedAt,omitempty"`
	Count       int        `json:"count"`
}

func NewAlerter(config *ConfigStore, monitor *Monitor, walg *WalG) *Alerter {
	a := &Alerter{
		config:  config,
		monitor: monitor,
		walg:    walg,
		active:  make(map[string]*AlertState),
	}
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
	seen := make(map[string]struct{})
	markSeen := func(key string) {
		if key != "" {
			seen[key] = struct{}{}
		}
	}

	// Disk usage
	if cfg.Thresholds.DiskUsagePct > 0 && stats.System.DiskUsage > float64(cfg.Thresholds.DiskUsagePct) {
		key := "disk_usage"
		markSeen(key)
		a.fire(cfg, key, "critical", "disk_usage",
			fmt.Sprintf("Disk usage %.1f%% exceeds %d%%", stats.System.DiskUsage, cfg.Thresholds.DiskUsagePct),
			fmt.Sprintf("%.1f%%", stats.System.DiskUsage))
	}

	// Connection usage
	if cfg.Thresholds.ConnectionsPct > 0 && stats.Connections.MaxConnections > 0 {
		pct := float64(stats.Connections.UsedConnections) / float64(stats.Connections.MaxConnections) * 100
		if pct > float64(cfg.Thresholds.ConnectionsPct) {
			key := "connections"
			markSeen(key)
			a.fire(cfg, key, "warning", "connections",
				fmt.Sprintf("Connection usage %.0f%% (%d/%d) exceeds %d%%",
					pct, stats.Connections.UsedConnections, stats.Connections.MaxConnections, cfg.Thresholds.ConnectionsPct),
				fmt.Sprintf("%.0f%%", pct))
		}
	}

	if cfg.Thresholds.ReplicationLagSec > 0 {
		for _, repl := range stats.Replication {
			if repl.ReplayLagSeconds > int64(cfg.Thresholds.ReplicationLagSec) {
				key := "replication_lag:" + repl.ClientAddr
				markSeen(key)
				a.fire(cfg, key, "warning", "replication_lag",
					fmt.Sprintf("Replication lag %ds on %s exceeds %ds",
						repl.ReplayLagSeconds, repl.ClientAddr, cfg.Thresholds.ReplicationLagSec),
					fmt.Sprintf("%ds", repl.ReplayLagSeconds))
			}
		}
	}

	if cfg.Thresholds.BackupMaxAgeHours > 0 && a.walg != nil {
		latest, err := a.walg.LatestBackupTime(context.Background())
		if err != nil {
			key := "backup_age"
			markSeen(key)
			a.fire(cfg, key, "warning", "backup_age",
				fmt.Sprintf("Failed to inspect backups: %v", err), "unknown")
		} else if latest.IsZero() {
			key := "backup_age"
			markSeen(key)
			a.fire(cfg, key, "critical", "backup_age", "No backups found", "none")
		} else {
			age := time.Since(latest)
			limit := time.Duration(cfg.Thresholds.BackupMaxAgeHours) * time.Hour
			if age > limit {
				key := "backup_age"
				markSeen(key)
				a.fire(cfg, key, "critical", "backup_age",
					fmt.Sprintf("Latest backup is %s old and exceeds %dh",
						age.Round(time.Minute), cfg.Thresholds.BackupMaxAgeHours),
					age.Round(time.Minute).String())
			}
		}
	}

	a.resolveMissing(seen)
}

func (a *Alerter) fire(cfg AlertConfig, key, level, metric, message, value string) {
	now := time.Now()
	cooldown := time.Duration(cfg.CooldownMinutes) * time.Minute

	a.mu.Lock()
	state, exists := a.active[key]
	if !exists {
		state = &AlertState{
			Key:      key,
			Metric:   metric,
			Level:    level,
			Status:   "open",
			OpenedAt: now,
			Count:    0,
		}
		a.active[key] = state
	}
	state.Metric = metric
	state.Level = level
	state.Status = "open"
	state.Message = message
	state.Value = value
	state.LastSeenAt = now
	state.Count++

	if cooldown > 0 && !state.LastFiredAt.IsZero() && now.Sub(state.LastFiredAt) < cooldown {
		a.mu.Unlock()
		return
	}
	state.LastFiredAt = now
	state.ResolvedAt = nil
	event := AlertEvent{
		Time:    now,
		Level:   level,
		Message: message,
		Metric:  metric,
		Value:   value,
		Key:     key,
		Status:  "open",
	}

	// Keep last 100 events
	a.history = append(a.history, event)
	if len(a.history) > 100 {
		a.history = a.history[len(a.history)-100:]
	}
	a.mu.Unlock()

	log.Printf("🚨 ALERT [%s] %s: %s", level, metric, message)

	// Send Telegram if configured
	if cfg.Telegram.BotToken != "" && cfg.Telegram.ChatID != "" {
		go a.sendTelegram(cfg.Telegram, fmt.Sprintf("🚨 PGAIO [%s]\n%s", strings.ToUpper(level), message))
	}
}

func (a *Alerter) resolveMissing(seen map[string]struct{}) {
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	for key, state := range a.active {
		if state.Status != "open" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		state.Status = "resolved"
		resolved := now
		state.ResolvedAt = &resolved
		event := AlertEvent{
			Time:    now,
			Level:   "resolved",
			Message: "Resolved: " + state.Message,
			Metric:  state.Metric,
			Value:   state.Value,
			Key:     key,
			Status:  "resolved",
		}
		a.history = append(a.history, event)
		if len(a.history) > 100 {
			a.history = a.history[len(a.history)-100:]
		}
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
	a.mu.RLock()
	defer a.mu.RUnlock()
	return append([]AlertEvent(nil), a.history...)
}

// GetStatus returns current alert status.
func (a *Alerter) GetStatus() map[string]any {
	cfg := a.config.GetAlerts()
	a.mu.RLock()
	history := append([]AlertEvent(nil), a.history...)
	active := make([]AlertState, 0, len(a.active))
	for _, state := range a.active {
		cp := *state
		if state.ResolvedAt != nil {
			resolved := *state.ResolvedAt
			cp.ResolvedAt = &resolved
		}
		active = append(active, cp)
	}
	a.mu.RUnlock()
	return map[string]any{
		"enabled":    cfg.Enabled,
		"thresholds": cfg.Thresholds,
		"cooldown":   cfg.CooldownMinutes,
		"telegram":   cfg.Telegram.BotToken != "",
		"history":    history,
		"active":     active,
	}
}

// MarshalJSON for AlertEvent
func init() {
	_ = json.Marshal
}
