package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolManager manages per-database connection pools.
// PostgreSQL catalog views only show data for the connected database,
// so we need separate pools for each database we want to inspect.
type PoolManager struct {
	defaultPool *pgxpool.Pool
	pools       map[string]*managedPool
	mu          sync.RWMutex
	baseConfig  *pgxpool.Config
	configStore *ConfigStore
	stopCh      chan struct{}
}

type managedPool struct {
	pool     *pgxpool.Pool
	lastUsed time.Time
}

const (
	poolManagerMaxPools   = 16
	poolManagerIdleTTL    = 10 * time.Minute
	poolManagerSweepEvery = 2 * time.Minute
)

// NewPoolManager creates a pool manager from the default pool's config.
func NewPoolManager(defaultPool *pgxpool.Pool, configStore *ConfigStore) *PoolManager {
	pm := &PoolManager{
		defaultPool: defaultPool,
		pools:       make(map[string]*managedPool),
		baseConfig:  defaultPool.Config().Copy(),
		configStore: configStore,
		stopCh:      make(chan struct{}),
	}
	go pm.sweepIdlePools()
	return pm
}

// GetPool returns a connection pool for the given database name.
// If database is empty, returns the default pool.
// Pools are cached and reused.
func (pm *PoolManager) GetPool(ctx context.Context, dbName string) (*pgxpool.Pool, error) {
	return pm.GetPoolForProfile(ctx, dbName, "")
}

func (pm *PoolManager) GetPoolForProfile(ctx context.Context, dbName, profileName string) (*pgxpool.Pool, error) {
	if profileName == "" || profileName == "direct-postgres" {
		// Check if this is the default database
		var currentDB string
		pm.defaultPool.QueryRow(ctx, "SELECT current_database()").Scan(&currentDB)
		if dbName == "" || dbName == currentDB {
			return pm.defaultPool, nil
		}
	} else if dbName == "" {
		dbName = pm.profileDatabase(profileName)
	}
	cacheKey := pm.poolKey(profileName, dbName)

	// Check cache
	pm.mu.RLock()
	if managed, ok := pm.pools[cacheKey]; ok {
		pool := managed.pool
		pm.mu.RUnlock()
		pm.touchPool(cacheKey)
		return pool, nil
	}
	pm.mu.RUnlock()

	// Create new pool
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Double-check after acquiring write lock
	if managed, ok := pm.pools[cacheKey]; ok {
		managed.lastUsed = time.Now()
		return managed.pool, nil
	}

	if len(pm.pools) >= poolManagerMaxPools {
		pm.evictOldestPoolLocked()
	}

	config, err := pm.resolveProfileConfig(profileName)
	if err != nil {
		return nil, err
	}
	if dbName != "" {
		config.ConnConfig.Database = dbName
	}
	config.MaxConns = 3
	config.MinConns = 0
	config.MinIdleConns = 0
	config.HealthCheckPeriod = 30 * time.Second
	config.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database %s: %w", dbName, err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database %s: %w", dbName, err)
	}

	pm.pools[cacheKey] = &managedPool{
		pool:     pool,
		lastUsed: time.Now(),
	}
	log.Printf("[pool-manager] created pool for database/profile: %s", cacheKey)
	return pool, nil
}

// DefaultPool returns the main connection pool.
func (pm *PoolManager) DefaultPool() *pgxpool.Pool {
	return pm.defaultPool
}

// Close closes all managed pools (not the default).
func (pm *PoolManager) Close() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	close(pm.stopCh)
	for name, managed := range pm.pools {
		managed.pool.Close()
		log.Printf("[pool-manager] closed pool for database: %s", name)
	}
	pm.pools = make(map[string]*managedPool)
}

// DefaultConnString returns a CLI-compatible connection string for the default database.
func (pm *PoolManager) DefaultConnString() string {
	config := pm.baseConfig.Copy()
	return config.ConnString()
}

// ConnStringForDB returns a CLI-compatible connection string for a specific database.
func (pm *PoolManager) ConnStringForDB(dbName string) string {
	return pm.ConnStringForProfile("", dbName)
}

func (pm *PoolManager) ConnStringForProfile(profileName, dbName string) string {
	config, err := pm.resolveProfileConfig(profileName)
	if err != nil {
		return ""
	}
	if dbName != "" {
		config.ConnConfig.Database = dbName
	}
	return config.ConnString()
}

func (pm *PoolManager) sweepIdlePools() {
	ticker := time.NewTicker(poolManagerSweepEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			pm.mu.Lock()
			now := time.Now()
			for name, managed := range pm.pools {
				if now.Sub(managed.lastUsed) < poolManagerIdleTTL {
					continue
				}
				managed.pool.Close()
				delete(pm.pools, name)
				log.Printf("[pool-manager] evicted idle pool for database: %s", name)
			}
			pm.mu.Unlock()
		case <-pm.stopCh:
			return
		}
	}
}

func (pm *PoolManager) evictOldestPoolLocked() {
	var oldestName string
	var oldestTime time.Time
	for name, managed := range pm.pools {
		if oldestName == "" || managed.lastUsed.Before(oldestTime) {
			oldestName = name
			oldestTime = managed.lastUsed
		}
	}
	if oldestName == "" {
		return
	}
	pm.pools[oldestName].pool.Close()
	delete(pm.pools, oldestName)
	log.Printf("[pool-manager] evicted least-recently-used pool for database: %s", oldestName)
}

func (pm *PoolManager) touchPool(cacheKey string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if managed, ok := pm.pools[cacheKey]; ok {
		managed.lastUsed = time.Now()
	}
}

func (pm *PoolManager) resolveProfileConfig(profileName string) (*pgxpool.Config, error) {
	if profileName == "" || profileName == "direct-postgres" {
		return pm.baseConfig.Copy(), nil
	}
	profile := pm.lookupProfile(profileName)
	if profile == nil {
		return nil, fmt.Errorf("connection profile not found: %s", profileName)
	}
	if !profile.Enabled {
		return nil, fmt.Errorf("connection profile disabled: %s", profileName)
	}

	connStr := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		profile.Host,
		profile.Port,
		getConnEnv("POSTGRESQL_USERNAME", "postgres"),
		getConnEnv("POSTGRESQL_PASSWORD", ""),
		profile.Database,
		profile.SSLMode,
	)
	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection profile %s: %w", profileName, err)
	}
	return config, nil
}

func (pm *PoolManager) lookupProfile(profileName string) *ConnectionProfile {
	if pm.configStore == nil {
		return nil
	}
	connections := pm.configStore.GetConnections()
	for _, profile := range connections.Profiles {
		if profile.Name == profileName {
			cp := profile
			return &cp
		}
	}
	return nil
}

func (pm *PoolManager) profileDatabase(profileName string) string {
	profile := pm.lookupProfile(profileName)
	if profile == nil {
		return ""
	}
	return profile.Database
}

func (pm *PoolManager) poolKey(profileName, dbName string) string {
	if profileName == "" {
		profileName = "direct-postgres"
	}
	if dbName == "" {
		dbName = "(default)"
	}
	return profileName + "|" + dbName
}

func getConnEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
