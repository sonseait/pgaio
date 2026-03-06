package service

import (
	"encoding/json"
	"log"
	"os"
	"sync"
)

const configFilePath = "/bitnami/postgresql/.pgaio_config.json"

// AppConfig holds all PGAIO application settings.
type AppConfig struct {
	Backup BackupConfig `json:"backup"`
	Alerts AlertConfig  `json:"alerts"`
}

type BackupConfig struct {
	Enabled       bool `json:"enabled"`
	IntervalHours int  `json:"intervalHours"`
	RetainCount   int  `json:"retainCount"`
}

type AlertConfig struct {
	Enabled    bool            `json:"enabled"`
	Telegram   TelegramConfig  `json:"telegram"`
	Thresholds AlertThresholds `json:"thresholds"`
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
		},
		Alerts: AlertConfig{
			Enabled: false,
			Thresholds: AlertThresholds{
				DiskUsagePct:      80,
				ConnectionsPct:    80,
				ReplicationLagSec: 30,
				BackupMaxAgeHours: 24,
			},
		},
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

func (cs *ConfigStore) load() error {
	data, err := os.ReadFile(configFilePath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &cs.config)
}

func (cs *ConfigStore) save() error {
	data, err := json.MarshalIndent(cs.config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configFilePath, data, 0644)
}
