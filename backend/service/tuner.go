package service

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
)

// WorkloadProfile defines the type of database workload.
type WorkloadProfile string

const (
	ProfileWeb     WorkloadProfile = "web"
	ProfileOLAP    WorkloadProfile = "olap"
	ProfileMixed   WorkloadProfile = "mixed"
	ProfileDesktop WorkloadProfile = "desktop"
)

// SystemInfo holds detected hardware/OS information.
type SystemInfo struct {
	TotalRAM    uint64  `json:"totalRam"`
	TotalRAMHR  string  `json:"totalRamHR"`
	CPUCores    int     `json:"cpuCores"`
	DiskType    string  `json:"diskType"` // "ssd" or "hdd"
	DiskTotal   uint64  `json:"diskTotal"`
	DiskTotalHR string  `json:"diskTotalHR"`
	PGVersion   string  `json:"pgVersion"`
	OSInfo      string  `json:"osInfo"`
}

// TuneRequest is the input for the tuning calculation.
type TuneRequest struct {
	Profile             WorkloadProfile `json:"profile"`
	ExpectedConnections int             `json:"expectedConnections"`
}

// ConfigRecommendation is a single tuning parameter recommendation.
type ConfigRecommendation struct {
	Name         string `json:"name"`
	CurrentValue string `json:"currentValue"`
	CurrentUnit  string `json:"currentUnit"`
	NewValue     string `json:"newValue"`
	Category     string `json:"category"`
	Description  string `json:"description"`
	NeedRestart  bool   `json:"needRestart"`
	Context      string `json:"context"` // postmaster, sighup, user, superuser
}

// ConnectionCalcResult holds connection calculation results.
type ConnectionCalcResult struct {
	PG        PGConnectionConfig        `json:"pg"`
	PgBouncer PgBouncerConnectionConfig `json:"pgbouncer"`
	Summary   string                    `json:"summary"`
}

// PGConnectionConfig is the PostgreSQL connection config recommendation.
type PGConnectionConfig struct {
	MaxConnections            int `json:"maxConnections"`
	SuperuserReservedConnections int `json:"superuserReservedConnections"`
}

// PgBouncerConnectionConfig is the PgBouncer recommendation.
type PgBouncerConnectionConfig struct {
	MaxClientConn    int `json:"maxClientConn"`
	DefaultPoolSize  int `json:"defaultPoolSize"`
	MinPoolSize      int `json:"minPoolSize"`
	ReservePoolSize  int `json:"reservePoolSize"`
	MaxDbConnections int `json:"maxDbConnections"`
}

// TuneResult is the full result of the tuning analysis.
type TuneResult struct {
	System          SystemInfo             `json:"system"`
	Profile         WorkloadProfile        `json:"profile"`
	Recommendations []ConfigRecommendation `json:"recommendations"`
	Connections     ConnectionCalcResult   `json:"connections"`
}

// Tuner performs PostgreSQL tuning analysis.
type Tuner struct {
	pool *pgxpool.Pool
}

// NewTuner creates a new Tuner.
func NewTuner(pool *pgxpool.Pool) *Tuner {
	return &Tuner{pool: pool}
}

// DetectSystem gathers current hardware and PostgreSQL info.
func (t *Tuner) DetectSystem(ctx context.Context) SystemInfo {
	info := SystemInfo{
		CPUCores: runtime.NumCPU(),
		DiskType: detectDiskType(),
	}

	// RAM
	if f, err := os.Open("/proc/meminfo"); err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			parts := strings.Fields(scanner.Text())
			if len(parts) >= 2 && strings.TrimSuffix(parts[0], ":") == "MemTotal" {
				val, _ := strconv.ParseUint(parts[1], 10, 64)
				info.TotalRAM = val * 1024 // kB to bytes
				break
			}
		}
	}
	info.TotalRAMHR = formatBytes(info.TotalRAM)

	// Disk total
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/bitnami/postgresql/data", &stat); err == nil {
		info.DiskTotal = stat.Blocks * uint64(stat.Bsize)
	}
	info.DiskTotalHR = formatBytes(info.DiskTotal)

	// PG version
	if t.pool != nil {
		t.pool.QueryRow(ctx, "SHOW server_version").Scan(&info.PGVersion)
	}

	// OS info
	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				info.OSInfo = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
				break
			}
		}
	}

	return info
}

// Analyze performs the full tuning analysis.
func (t *Tuner) Analyze(ctx context.Context, req TuneRequest) (*TuneResult, error) {
	system := t.DetectSystem(ctx)

	// Get current settings
	currentSettings, err := t.getCurrentSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get current settings: %w", err)
	}

	// Calculate recommendations
	recommendations := t.calculate(system, req, currentSettings)

	// Calculate connection config
	connections := t.calculateConnections(system, req)

	return &TuneResult{
		System:          system,
		Profile:         req.Profile,
		Recommendations: recommendations,
		Connections:     connections,
	}, nil
}

type pgSetting struct {
	value   string
	unit    string
	context string
}

func (t *Tuner) getCurrentSettings(ctx context.Context) (map[string]pgSetting, error) {
	rows, err := t.pool.Query(ctx, `
		SELECT name, setting, COALESCE(unit, ''), context
		FROM pg_settings
		WHERE name IN (
			'shared_buffers', 'effective_cache_size', 'work_mem',
			'maintenance_work_mem', 'wal_buffers', 'min_wal_size', 'max_wal_size',
			'checkpoint_completion_target', 'checkpoint_timeout',
			'max_worker_processes', 'max_parallel_workers_per_gather',
			'max_parallel_workers', 'max_parallel_maintenance_workers',
			'random_page_cost', 'effective_io_concurrency',
			'huge_pages', 'max_connections', 'superuser_reserved_connections',
			'default_statistics_target', 'wal_level'
		)
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := make(map[string]pgSetting)
	for rows.Next() {
		var name, value, unit, context string
		if err := rows.Scan(&name, &value, &unit, &context); err != nil {
			continue
		}
		settings[name] = pgSetting{value: value, unit: unit, context: context}
	}
	return settings, nil
}

func (t *Tuner) calculate(sys SystemInfo, req TuneRequest, cur map[string]pgSetting) []ConfigRecommendation {
	ramBytes := sys.TotalRAM
	cpuCores := sys.CPUCores
	isSSD := sys.DiskType == "ssd"
	profile := req.Profile

	if profile == "" {
		profile = ProfileWeb
	}

	var recs []ConfigRecommendation

	// --- Memory Settings ---

	// shared_buffers: RAM / 4
	sharedBuffers := ramBytes / 4
	// Cap at reasonable value based on profile
	if profile == ProfileDesktop {
		sharedBuffers = ramBytes / 16
	}
	recs = append(recs, makeRec("shared_buffers", cur, formatPGMem(sharedBuffers),
		"Resource Usage / Memory", "Main shared memory area for caching data", "postmaster"))

	// effective_cache_size: RAM * 3/4
	effectiveCache := ramBytes * 3 / 4
	if profile == ProfileDesktop {
		effectiveCache = ramBytes / 4
	}
	recs = append(recs, makeRec("effective_cache_size", cur, formatPGMem(effectiveCache),
		"Query Tuning / Planner", "Estimate of OS cache available for a single query", "user"))

	// work_mem
	maxConn := req.ExpectedConnections
	if maxConn <= 0 {
		maxConn = 100
	}
	// For PgBouncer setup, actual PG connections are much lower
	pgMaxConn := calculatePGMaxConn(maxConn, cpuCores)
	workMem := (ramBytes - sharedBuffers) / uint64(pgMaxConn*3)
	// Apply profile multiplier
	switch profile {
	case ProfileOLAP:
		workMem = workMem * 2
	case ProfileDesktop:
		workMem = ramBytes / 16 / uint64(pgMaxConn)
	}
	// Minimum 4MB
	if workMem < 4*1024*1024 {
		workMem = 4 * 1024 * 1024
	}
	recs = append(recs, makeRec("work_mem", cur, formatPGMem(workMem),
		"Resource Usage / Memory", "Memory used for sort operations and hash tables per operation", "user"))

	// maintenance_work_mem: RAM / 16 (max 2GB)
	maintWorkMem := ramBytes / 16
	maxMaintMem := uint64(2) * 1024 * 1024 * 1024
	if maintWorkMem > maxMaintMem {
		maintWorkMem = maxMaintMem
	}
	if profile == ProfileDesktop {
		maintWorkMem = ramBytes / 16
		if maintWorkMem > 512*1024*1024 {
			maintWorkMem = 512 * 1024 * 1024
		}
	}
	recs = append(recs, makeRec("maintenance_work_mem", cur, formatPGMem(maintWorkMem),
		"Resource Usage / Memory", "Memory for maintenance operations (VACUUM, CREATE INDEX)", "user"))

	// --- WAL Settings ---

	// wal_buffers: 3% of shared_buffers (min 64kB, max 64MB)
	walBuffers := sharedBuffers * 3 / 100
	if walBuffers < 64*1024 {
		walBuffers = 64 * 1024
	}
	if walBuffers > 64*1024*1024 {
		walBuffers = 64 * 1024 * 1024
	}
	// Round to nearest MB
	walBuffers = (walBuffers / (1024 * 1024)) * 1024 * 1024
	if walBuffers < 1024*1024 {
		walBuffers = 1024 * 1024
	}
	recs = append(recs, makeRec("wal_buffers", cur, formatPGMem(walBuffers),
		"WAL / Settings", "Shared memory for WAL data not yet written to disk", "postmaster"))

	// min_wal_size
	var minWalSize, maxWalSize string
	switch profile {
	case ProfileWeb:
		minWalSize = "1GB"
		maxWalSize = "4GB"
	case ProfileOLAP:
		minWalSize = "4GB"
		maxWalSize = "16GB"
	case ProfileMixed:
		minWalSize = "2GB"
		maxWalSize = "8GB"
	case ProfileDesktop:
		minWalSize = "100MB"
		maxWalSize = "2GB"
	}
	recs = append(recs, makeRec("min_wal_size", cur, minWalSize,
		"WAL / Checkpoints", "Minimum size of WAL files", "sighup"))
	recs = append(recs, makeRec("max_wal_size", cur, maxWalSize,
		"WAL / Checkpoints", "Maximum size of WAL files before checkpoint", "sighup"))

	// --- Checkpoint Settings ---

	recs = append(recs, makeRec("checkpoint_completion_target", cur, "0.9",
		"WAL / Checkpoints", "Target for checkpoint completion as fraction of interval", "sighup"))

	if profile == ProfileOLAP || profile == ProfileMixed {
		recs = append(recs, makeRec("checkpoint_timeout", cur, "15min",
			"WAL / Checkpoints", "Maximum time between automatic WAL checkpoints", "sighup"))
	}

	// --- Parallelism (CPU-based) ---

	recs = append(recs, makeRec("max_worker_processes", cur, fmt.Sprintf("%d", cpuCores),
		"Resource Usage / Asynchronous", "Maximum number of concurrent worker processes", "postmaster"))

	parallelGather := cpuCores / 2
	if parallelGather < 1 {
		parallelGather = 1
	}
	if profile == ProfileDesktop {
		parallelGather = 1
	}
	if parallelGather > 4 {
		parallelGather = 4
	}
	recs = append(recs, makeRec("max_parallel_workers_per_gather", cur, fmt.Sprintf("%d", parallelGather),
		"Resource Usage / Asynchronous", "Max workers that can be started by a single Gather", "user"))

	maxParallel := cpuCores
	if profile == ProfileDesktop {
		maxParallel = 2
	}
	recs = append(recs, makeRec("max_parallel_workers", cur, fmt.Sprintf("%d", maxParallel),
		"Resource Usage / Asynchronous", "Max workers for parallel operations", "user"))

	maintParallel := cpuCores / 2
	if maintParallel < 1 {
		maintParallel = 1
	}
	if profile == ProfileDesktop {
		maintParallel = 1
	}
	if maintParallel > 4 {
		maintParallel = 4
	}
	recs = append(recs, makeRec("max_parallel_maintenance_workers", cur, fmt.Sprintf("%d", maintParallel),
		"Resource Usage / Asynchronous", "Max workers for parallel maintenance (VACUUM, CREATE INDEX)", "user"))

	// --- Disk Settings ---

	if isSSD {
		recs = append(recs, makeRec("random_page_cost", cur, "1.1",
			"Query Tuning / Planner Cost", "Estimated cost of a non-sequential disk page fetch (SSD optimized)", "user"))
		recs = append(recs, makeRec("effective_io_concurrency", cur, "200",
			"Resource Usage / Asynchronous", "Number of concurrent disk I/O operations (SSD optimized)", "user"))
	} else {
		recs = append(recs, makeRec("random_page_cost", cur, "4.0",
			"Query Tuning / Planner Cost", "Estimated cost of a non-sequential disk page fetch", "user"))
		recs = append(recs, makeRec("effective_io_concurrency", cur, "2",
			"Resource Usage / Asynchronous", "Number of concurrent disk I/O operations", "user"))
	}

	// huge_pages
	if ramBytes > 32*1024*1024*1024 { // > 32GB
		recs = append(recs, makeRec("huge_pages", cur, "try",
			"Resource Usage / Memory", "Use huge pages when available (recommended for >32GB RAM)", "postmaster"))
	} else {
		recs = append(recs, makeRec("huge_pages", cur, "off",
			"Resource Usage / Memory", "Huge pages disabled (RAM <= 32GB)", "postmaster"))
	}

	// --- Connection Settings ---

	recs = append(recs, makeRec("max_connections", cur, fmt.Sprintf("%d", pgMaxConn),
		"Connections / Authentication", "Maximum concurrent connections (optimized for PgBouncer)", "postmaster"))

	// --- Statistics ---

	statTarget := "100"
	if profile == ProfileOLAP {
		statTarget = "500"
	}
	recs = append(recs, makeRec("default_statistics_target", cur, statTarget,
		"Query Tuning / Other", "Default statistics target for ANALYZE (higher = better plans, slower ANALYZE)", "user"))

	return recs
}

func (t *Tuner) calculateConnections(sys SystemInfo, req TuneRequest) ConnectionCalcResult {
	expectedConn := req.ExpectedConnections
	if expectedConn <= 0 {
		expectedConn = 100
	}

	cpuCores := sys.CPUCores
	pgMaxConn := calculatePGMaxConn(expectedConn, cpuCores)
	superuserReserved := 3

	// PgBouncer settings
	defaultPoolSize := (pgMaxConn - superuserReserved) / 2
	if defaultPoolSize < 5 {
		defaultPoolSize = 5
	}
	minPoolSize := defaultPoolSize / 4
	if minPoolSize < 2 {
		minPoolSize = 2
	}
	reservePoolSize := defaultPoolSize / 4
	if reservePoolSize < 2 {
		reservePoolSize = 2
	}
	maxDbConn := pgMaxConn - superuserReserved

	summary := fmt.Sprintf(
		"With %d expected app connections, PgBouncer will multiplex them into %d PostgreSQL connections "+
			"(pool_size=%d per database). This gives a %dx multiplexing ratio, significantly reducing PostgreSQL resource usage.",
		expectedConn, pgMaxConn, defaultPoolSize, expectedConn/pgMaxConn,
	)

	return ConnectionCalcResult{
		PG: PGConnectionConfig{
			MaxConnections:            pgMaxConn,
			SuperuserReservedConnections: superuserReserved,
		},
		PgBouncer: PgBouncerConnectionConfig{
			MaxClientConn:    expectedConn,
			DefaultPoolSize:  defaultPoolSize,
			MinPoolSize:      minPoolSize,
			ReservePoolSize:  reservePoolSize,
			MaxDbConnections: maxDbConn,
		},
		Summary: summary,
	}
}

// calculatePGMaxConn determines optimal PostgreSQL max_connections given expected app connections and CPU.
func calculatePGMaxConn(expectedConn, cpuCores int) int {
	// With PgBouncer transaction pooling, PG needs far fewer connections.
	// Rule: max(cpu_cores * 4, 50), but never more than expected/2 or 500.
	pgConn := cpuCores * 4
	if pgConn < 50 {
		pgConn = 50
	}
	// Don't exceed half of expected connections or hard cap
	cap := expectedConn / 2
	if cap < 50 {
		cap = 50
	}
	if cap > 500 {
		cap = 500
	}
	if pgConn > cap {
		pgConn = cap
	}
	return pgConn
}

// detectDiskType checks if the primary disk is SSD or HDD.
func detectDiskType() string {
	// Try to detect via sysfs
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return "ssd" // Assume SSD in containers
	}
	for _, entry := range entries {
		name := entry.Name()
		// Skip loop, ram, dm devices
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") {
			continue
		}
		rotational := fmt.Sprintf("/sys/block/%s/queue/rotational", name)
		data, err := os.ReadFile(rotational)
		if err != nil {
			continue
		}
		val := strings.TrimSpace(string(data))
		if val == "1" {
			return "hdd"
		}
		return "ssd"
	}
	return "ssd" // Default to SSD (containers/VMs typically use SSD-backed storage)
}

// formatPGMem formats bytes into PostgreSQL config format (e.g., "4GB", "256MB").
func formatPGMem(bytes uint64) string {
	gb := float64(bytes) / (1024 * 1024 * 1024)
	mb := float64(bytes) / (1024 * 1024)
	kb := float64(bytes) / 1024

	if gb >= 1 && math.Mod(gb, 1) < 0.01 {
		return fmt.Sprintf("%dGB", int(gb))
	}
	if mb >= 1 && math.Mod(mb, 1) < 0.01 {
		return fmt.Sprintf("%dMB", int(mb))
	}
	return fmt.Sprintf("%dkB", int(kb))
}

func makeRec(name string, cur map[string]pgSetting, newVal, category, desc, settingContext string) ConfigRecommendation {
	rec := ConfigRecommendation{
		Name:        name,
		NewValue:    newVal,
		Category:    category,
		Description: desc,
		Context:     settingContext,
		NeedRestart: settingContext == "postmaster",
	}
	if s, ok := cur[name]; ok {
		rec.CurrentValue = formatCurrentSetting(s.value, s.unit)
		rec.CurrentUnit = s.unit
	}
	return rec
}

// formatCurrentSetting makes pg_settings values human-readable.
func formatCurrentSetting(value, unit string) string {
	if unit == "" {
		return value
	}
	// pg_settings stores memory values in 8kB or kB units
	if unit == "8kB" {
		n, err := strconv.ParseInt(value, 10, 64)
		if err == nil {
			bytes := uint64(n) * 8 * 1024
			return formatPGMem(bytes)
		}
	}
	if unit == "kB" {
		n, err := strconv.ParseInt(value, 10, 64)
		if err == nil {
			bytes := uint64(n) * 1024
			return formatPGMem(bytes)
		}
	}
	if unit == "MB" {
		n, err := strconv.ParseInt(value, 10, 64)
		if err == nil {
			return formatPGMem(uint64(n) * 1024 * 1024)
		}
	}
	if unit == "min" || unit == "s" || unit == "ms" {
		return value + unit
	}
	return value
}

func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	exp := 0
	val := float64(b)
	for val >= unit && exp < 4 {
		val /= unit
		exp++
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	return fmt.Sprintf("%.1f %s", val, units[exp])
}
