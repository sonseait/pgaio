package service

import (
	"encoding/json"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
)

const configFilePath = "/bitnami/postgresql/.pgaio_config.json"

// AppConfig holds all PGAIO application settings.
type AppConfig struct {
	Backup      BackupConfig       `json:"backup"`
	Alerts      AlertConfig        `json:"alerts"`
	Connections ConnectionSettings `json:"connections"`
}

type BackupConfig struct {
	Enabled       bool `json:"enabled"`
	IntervalHours int  `json:"intervalHours"`
	RetainCount   int  `json:"retainCount"`
	VerifyEnabled bool `json:"verifyEnabled"`
}

type AlertConfig struct {
	Enabled         bool            `json:"enabled"`
	Telegram        TelegramConfig  `json:"telegram"`
	Thresholds      AlertThresholds `json:"thresholds"`
	CooldownMinutes int             `json:"cooldownMinutes"`
}

type TelegramConfig struct {
	BotToken string `json:"botToken"`
	ChatID   string `json:"chatId"`
}

type AlertThresholds struct {
	DiskUsagePct      int `json:"diskUsagePct"`
	ConnectionsPct    int `json:"connectionsPct"`
	ReplicationLagSec int `json:"replicationLagSec"`
	BackupMaxAgeHours int `json:"backupMaxAgeHours"`
}

type ConnectionSettings struct {
	Profiles      []ConnectionProfile `json:"profiles"`
	FeatureRoutes map[string]string   `json:"featureRoutes"`
}

type ConnectionProfile struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Type        string `json:"type"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Database    string `json:"database"`
	SSLMode     string `json:"sslMode"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

// ConfigStore manages persistent application configuration.
type ConfigStore struct {
	mu     sync.RWMutex
	config AppConfig
}

func defaultConfig() AppConfig {
	return AppConfig{
		Backup: BackupConfig{
			Enabled:       true,
			IntervalHours: 6,
			RetainCount:   7,
			VerifyEnabled: true,
		},
		Alerts: AlertConfig{
			Enabled:         false,
			CooldownMinutes: 15,
			Thresholds: AlertThresholds{
				DiskUsagePct:      80,
				ConnectionsPct:    80,
				ReplicationLagSec: 30,
				BackupMaxAgeHours: 24,
			},
		},
		Connections: defaultConnections(),
	}
}

// NewConfigStore creates a config store, loading from disk or creating defaults.
func NewConfigStore() *ConfigStore {
	cs := &ConfigStore{config: defaultConfig()}
	if err := cs.load(); err != nil {
		log.Printf("⚙️  Config: creating default config (%v)", err)
		if err := cs.save(); err != nil {
			log.Printf("⚠️  Config: failed to save defaults: %v", err)
		}
	}
	log.Println("⚙️  Config store loaded")
	return cs
}

// Get returns a copy of the current config.
func (cs *ConfigStore) Get() AppConfig {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.config
}

// Update applies a new config and persists it.
func (cs *ConfigStore) Update(cfg AppConfig) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.config = cfg
	cs.applyConnectionDefaultsLocked()
	return cs.save()
}

// GetBackup returns backup config.
func (cs *ConfigStore) GetBackup() BackupConfig {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.config.Backup
}

// GetAlerts returns alert config.
func (cs *ConfigStore) GetAlerts() AlertConfig {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.config.Alerts
}

func (cs *ConfigStore) GetConnections() ConnectionSettings {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.config.Connections
}

func (cs *ConfigStore) load() error {
	data, err := os.ReadFile(configFilePath)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &cs.config); err != nil {
		return err
	}
	cs.applyConnectionDefaultsLocked()
	return nil
}

func (cs *ConfigStore) save() error {
	data, err := json.MarshalIndent(cs.config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configFilePath, data, 0644)
}

func defaultConnections() ConnectionSettings {
	host := getConfigEnv("PGHOST", "/tmp")
	port := getConfigEnvInt("PGPORT", 5432)
	db := getConfigEnv("POSTGRESQL_DATABASE", "postgres")
	pgbHost, pgbPort := splitHostPort(getConfigEnv("PGBOUNCER_ADMIN_ADDR", "127.0.0.1:6432"), "127.0.0.1", 6432)

	return ConnectionSettings{
		Profiles: []ConnectionProfile{
			{
				Name:        "direct-postgres",
				Label:       "direct postgres",
				Type:        "postgres",
				Host:        host,
				Port:        port,
				Database:    db,
				SSLMode:     "disable",
				Description: "Direct PostgreSQL connection using the primary server endpoint.",
				Enabled:     true,
			},
			{
				Name:        "pgbouncer-transaction",
				Label:       "pgbouncer",
				Type:        "pgbouncer",
				Host:        pgbHost,
				Port:        pgbPort,
				Database:    db,
				SSLMode:     "disable",
				Description: "Transaction-pooled PgBouncer route for lightweight app traffic.",
				Enabled:     true,
			},
		},
		FeatureRoutes: map[string]string{
			"sql":         "direct-postgres",
			"queries":     "direct-postgres",
			"maintenance": "direct-postgres",
			"drift":       "direct-postgres",
		},
	}
}

func (cs *ConfigStore) applyConnectionDefaultsLocked() {
	defaults := defaultConnections()
	if len(cs.config.Connections.Profiles) == 0 {
		cs.config.Connections.Profiles = defaults.Profiles
	}
	if cs.config.Connections.FeatureRoutes == nil {
		cs.config.Connections.FeatureRoutes = defaults.FeatureRoutes
	}
	if cs.config.Alerts.CooldownMinutes <= 0 {
		cs.config.Alerts.CooldownMinutes = 15
	}
	for key, value := range defaults.FeatureRoutes {
		if strings.TrimSpace(cs.config.Connections.FeatureRoutes[key]) == "" {
			cs.config.Connections.FeatureRoutes[key] = value
		}
	}
	for i := range cs.config.Connections.Profiles {
		profile := &cs.config.Connections.Profiles[i]
		if profile.Label == "" {
			profile.Label = profile.Name
		}
		if profile.Type == "" {
			profile.Type = "postgres"
		}
		if profile.Port == 0 {
			profile.Port = defaults.Profiles[0].Port
		}
		if profile.Database == "" {
			profile.Database = defaults.Profiles[0].Database
		}
		if profile.SSLMode == "" {
			profile.SSLMode = "disable"
		}
		if !profile.Enabled {
			profile.Enabled = profile.Name != ""
		}
	}
}

func getConfigEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getConfigEnvInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func splitHostPort(addr, defHost string, defPort int) (string, int) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return defHost, defPort
	}
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		return addr, defPort
	}
	port, err := strconv.Atoi(parts[1])
	if err != nil {
		port = defPort
	}
	return parts[0], port
}
