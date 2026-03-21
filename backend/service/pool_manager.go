package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolManager manages per-database connection pools.
// PostgreSQL catalog views only show data for the connected database,
// so we need separate pools for each database we want to inspect.
type PoolManager struct {
	defaultPool *pgxpool.Pool
	pools       map[string]*pgxpool.Pool
	mu          sync.RWMutex
	baseConfig  *pgxpool.Config
}

// NewPoolManager creates a pool manager from the default pool's config.
func NewPoolManager(defaultPool *pgxpool.Pool) *PoolManager {
	return &PoolManager{
		defaultPool: defaultPool,
		pools:       make(map[string]*pgxpool.Pool),
	}
}

// GetPool returns a connection pool for the given database name.
// If database is empty, returns the default pool.
// Pools are cached and reused.
func (pm *PoolManager) GetPool(ctx context.Context, dbName string) (*pgxpool.Pool, error) {
	if dbName == "" {
		return pm.defaultPool, nil
	}

	// Check if this is the default database
	var currentDB string
	pm.defaultPool.QueryRow(ctx, "SELECT current_database()").Scan(&currentDB)
	if dbName == currentDB {
		return pm.defaultPool, nil
	}

	// Check cache
	pm.mu.RLock()
	if pool, ok := pm.pools[dbName]; ok {
		pm.mu.RUnlock()
		return pool, nil
	}
	pm.mu.RUnlock()

	// Create new pool
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Double-check after acquiring write lock
	if pool, ok := pm.pools[dbName]; ok {
		return pool, nil
	}

	// Build connection string for the target database
	host := getPoolEnv("PGHOST", "/tmp")
	port := getPoolEnv("PGPORT", "5432")
	user := getPoolEnv("POSTGRESQL_USERNAME", "postgres")
	pass := getPoolEnv("POSTGRESQL_PASSWORD", "postgres")

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, pass, dbName)

	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config for database %s: %w", dbName, err)
	}
	config.MaxConns = 3
	config.MinConns = 0

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database %s: %w", dbName, err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database %s: %w", dbName, err)
	}

	pm.pools[dbName] = pool
	log.Printf("[pool-manager] created pool for database: %s", dbName)
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
	for name, pool := range pm.pools {
		pool.Close()
		log.Printf("[pool-manager] closed pool for database: %s", name)
	}
	pm.pools = make(map[string]*pgxpool.Pool)
}

// DefaultConnString returns a CLI-compatible connection string for the default database.
func (pm *PoolManager) DefaultConnString() string {
	host := getPoolEnv("PGHOST", "/tmp")
	port := getPoolEnv("PGPORT", "5432")
	user := getPoolEnv("POSTGRESQL_USERNAME", "postgres")
	pass := getPoolEnv("POSTGRESQL_PASSWORD", "")
	db := getPoolEnv("POSTGRESQL_DATABASE", "postgres")

	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, pass, db)
}

// ConnStringForDB returns a CLI-compatible connection string for a specific database.
func (pm *PoolManager) ConnStringForDB(dbName string) string {
	host := getPoolEnv("PGHOST", "/tmp")
	port := getPoolEnv("PGPORT", "5432")
	user := getPoolEnv("POSTGRESQL_USERNAME", "postgres")
	pass := getPoolEnv("POSTGRESQL_PASSWORD", "")

	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, pass, dbName)
}

func getPoolEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
