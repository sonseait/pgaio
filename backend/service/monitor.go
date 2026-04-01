package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"pgaio/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Monitor collects PostgreSQL and system statistics.
type Monitor struct {
	pool         *pgxpool.Pool
	systemMu     sync.RWMutex
	prevCPU      cpuTimes
	lastSystem   model.SystemStats
	lastSystemAt time.Time
	walMu        sync.Mutex
	prevWalLSN   uint64
	prevWalAt    time.Time
}

type cpuTimes struct {
	user, nice, system, idle, iowait, irq, softirq, steal uint64
	total, idleTotal                                      uint64
}

func NewMonitor(pool *pgxpool.Pool) *Monitor {
	m := &Monitor{pool: pool}
	m.prevCPU, _ = readCPUTimes()
	m.refreshSystemStats()
	go m.systemSampler()
	return m
}

func (m *Monitor) systemSampler() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		m.refreshSystemStats()
	}
}

// CollectStats gathers all PostgreSQL and system statistics.
func (m *Monitor) CollectStats(ctx context.Context) (*model.PgStat, error) {
	stat := &model.PgStat{
		Timestamp:        time.Now(),
		CollectionErrors: make(map[string]string),
	}

	// Database stats (all non-template databases)
	dbs, err := m.getDatabaseStats(ctx)
	if err == nil {
		stat.Databases = dbs
	} else {
		stat.CollectionErrors["databases"] = err.Error()
	}

	// Activity
	activity, err := m.getActivityStats(ctx)
	if err == nil {
		stat.Activity = activity
	} else {
		stat.CollectionErrors["activity"] = err.Error()
	}

	// Connections
	conns, err := m.getConnectionStats(ctx)
	if err == nil {
		stat.Connections = conns
	} else {
		stat.CollectionErrors["connections"] = err.Error()
	}

	// Replication
	repl, err := m.getReplicationStats(ctx)
	if err == nil {
		stat.Replication = repl
	} else {
		stat.CollectionErrors["replication"] = err.Error()
	}

	wal, err := m.getWALStats(ctx)
	if err == nil {
		stat.WAL = wal
	} else {
		stat.CollectionErrors["wal"] = err.Error()
	}

	bgwriter, err := m.getBGWriterStats(ctx)
	if err == nil {
		stat.BGWriter = bgwriter
	} else {
		stat.CollectionErrors["bgwriter"] = err.Error()
	}

	// System
	stat.System = m.getSystemStats()
	if len(stat.CollectionErrors) == 0 {
		stat.CollectionErrors = nil
	}

	return stat, nil
}

func (m *Monitor) getDatabaseStats(ctx context.Context) ([]model.DatabaseStats, error) {
	rows, err := m.pool.Query(ctx, `
		SELECT
			d.datname,
			pg_size_pretty(pg_database_size(d.datname)),
			pg_database_size(d.datname),
			COALESCE(s.numbackends, 0),
			COALESCE(s.xact_commit, 0),
			COALESCE(s.xact_rollback, 0),
			COALESCE(s.blks_read, 0),
			COALESCE(s.blks_hit, 0),
			CASE WHEN COALESCE(s.blks_hit, 0) + COALESCE(s.blks_read, 0) = 0 THEN 0
				ELSE round(COALESCE(s.blks_hit, 0)::numeric / (COALESCE(s.blks_hit, 0) + COALESCE(s.blks_read, 0))::numeric * 100, 2)
			END,
			COALESCE(s.blk_read_time, 0),
			COALESCE(s.blk_write_time, 0),
			COALESCE(s.temp_files, 0),
			COALESCE(s.temp_bytes, 0),
			COALESCE(s.deadlocks, 0),
			COALESCE(s.conflicts, 0),
			COALESCE(s.checksum_failures, 0),
			COALESCE(s.tup_returned, 0),
			COALESCE(s.tup_fetched, 0),
			COALESCE(s.tup_inserted, 0),
			COALESCE(s.tup_updated, 0),
			COALESCE(s.tup_deleted, 0)
		FROM pg_database d
		LEFT JOIN pg_stat_database s ON s.datname = d.datname
		WHERE d.datistemplate = false
		ORDER BY pg_database_size(d.datname) DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []model.DatabaseStats
	for rows.Next() {
		var s model.DatabaseStats
		if err := rows.Scan(
			&s.Name, &s.Size, &s.SizeBytes,
			&s.NumBackends,
			&s.TxCommit, &s.TxRollback,
			&s.BlksRead, &s.BlksHit, &s.CacheHitRatio,
			&s.BlkReadTime, &s.BlkWriteTime,
			&s.TempFiles, &s.TempBytes,
			&s.Deadlocks, &s.Conflicts,
			&s.ChecksumFailures,
			&s.TupReturned, &s.TupFetched,
			&s.TupInserted, &s.TupUpdated, &s.TupDeleted,
		); err != nil {
			continue
		}
		stats = append(stats, s)
	}
	if stats == nil {
		stats = []model.DatabaseStats{}
	}
	return stats, nil
}

func (m *Monitor) getActivityStats(ctx context.Context) (model.ActivityStats, error) {
	var a model.ActivityStats

	// Counts
	err := m.pool.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE state = 'active'),
			count(*) FILTER (WHERE state = 'idle'),
			count(*) FILTER (WHERE state = 'idle in transaction'),
			count(*) FILTER (WHERE state = 'active' AND age(clock_timestamp(), query_start) > interval '60 seconds'),
			count(*) FILTER (WHERE wait_event IS NOT NULL AND state = 'active')
		FROM pg_stat_activity
		WHERE backend_type = 'client backend' AND pid != pg_backend_pid()
	`).Scan(&a.TotalConnections, &a.ActiveQueries, &a.IdleConnections, &a.IdleInTransaction, &a.LongRunningQueries, &a.WaitingQueries)
	if err != nil {
		return a, err
	}

	_ = m.pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(EXTRACT(EPOCH FROM age(clock_timestamp(), query_start)) * 1000), 0)::bigint
		FROM pg_stat_activity
		WHERE backend_type = 'client backend' AND state = 'active' AND pid != pg_backend_pid()
	`).Scan(&a.OldestQueryMs)

	_ = m.pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(EXTRACT(EPOCH FROM age(clock_timestamp(), xact_start)) * 1000), 0)::bigint
		FROM pg_stat_activity
		WHERE backend_type = 'client backend'
		  AND state = 'idle in transaction'
		  AND xact_start IS NOT NULL
		  AND pid != pg_backend_pid()
	`).Scan(&a.OldestIdleInXactMs)

	waitRows, err := m.pool.Query(ctx, `
		SELECT
			COALESCE(wait_event_type, ''),
			COALESCE(wait_event, ''),
			count(*)
		FROM pg_stat_activity
		WHERE backend_type = 'client backend'
		  AND state = 'active'
		  AND wait_event IS NOT NULL
		  AND pid != pg_backend_pid()
		GROUP BY wait_event_type, wait_event
		ORDER BY count(*) DESC, wait_event_type, wait_event
		LIMIT 5
	`)
	if err == nil {
		defer waitRows.Close()
		for waitRows.Next() {
			var item model.WaitEventStat
			if err := waitRows.Scan(&item.Type, &item.Event, &item.Count); err == nil {
				a.WaitEvents = append(a.WaitEvents, item)
			}
		}
	}

	// Active queries detail
	rows, err := m.pool.Query(ctx, `
		SELECT
			pid,
			COALESCE(usename, ''),
			COALESCE(datname, ''),
			COALESCE(state, 'unknown'),
			COALESCE(LEFT(query, 200), ''),
			COALESCE(EXTRACT(EPOCH FROM age(clock_timestamp(), query_start)) * 1000, 0)::bigint,
			COALESCE(wait_event, ''),
			COALESCE(backend_type, ''),
			COALESCE(query_start, now())
		FROM pg_stat_activity
		WHERE backend_type = 'client backend' AND state = 'active' AND pid != pg_backend_pid()
		ORDER BY query_start ASC
		LIMIT 50
	`)
	if err != nil {
		return a, err
	}
	defer rows.Close()

	for rows.Next() {
		var q model.ActiveQuery
		var durationMs float64
		if err := rows.Scan(&q.PID, &q.User, &q.Database, &q.State, &q.Query, &durationMs, &q.WaitEvent, &q.BackendType, &q.QueryStart); err != nil {
			continue
		}
		q.Duration = int64(durationMs)
		a.Queries = append(a.Queries, q)
	}
	if a.WaitEvents == nil {
		a.WaitEvents = []model.WaitEventStat{}
	}
	if a.Queries == nil {
		a.Queries = []model.ActiveQuery{}
	}
	return a, nil
}

func (m *Monitor) getConnectionStats(ctx context.Context) (model.ConnectionStats, error) {
	var c model.ConnectionStats
	err := m.pool.QueryRow(ctx, `
		SELECT
			setting::int AS max_conn,
			(SELECT count(*) FROM pg_stat_activity) AS used,
			GREATEST(setting::int - (SELECT count(*) FROM pg_stat_activity), 0) AS available_total,
			GREATEST(
				setting::int -
				(SELECT count(*) FROM pg_stat_activity) -
				(SELECT setting::int FROM pg_settings WHERE name = 'superuser_reserved_connections'),
				0
			) AS available_non_superuser,
			(SELECT setting::int FROM pg_settings WHERE name = 'superuser_reserved_connections') AS reserved
		FROM pg_settings
		WHERE name = 'max_connections'
	`).Scan(&c.MaxConnections, &c.UsedConnections, &c.AvailableTotal, &c.AvailableConnections, &c.ReservedConnections)
	return c, err
}

func (m *Monitor) getReplicationStats(ctx context.Context) ([]model.ReplicationLag, error) {
	rows, err := m.pool.Query(ctx, `
		SELECT
			COALESCE(client_addr::text, 'local'),
			COALESCE(state, ''),
			COALESCE(pg_wal_lsn_diff(pg_current_wal_lsn(), sent_lsn), 0)::bigint,
			COALESCE(pg_wal_lsn_diff(pg_current_wal_lsn(), write_lsn), 0)::bigint,
			COALESCE(pg_wal_lsn_diff(pg_current_wal_lsn(), flush_lsn), 0)::bigint,
			COALESCE(pg_wal_lsn_diff(pg_current_wal_lsn(), replay_lsn), 0)::bigint,
			COALESCE(EXTRACT(EPOCH FROM replay_lag), 0)::bigint
		FROM pg_stat_replication
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []model.ReplicationLag
	for rows.Next() {
		var r model.ReplicationLag
		if err := rows.Scan(
			&r.ClientAddr,
			&r.State,
			&r.SentLagBytes,
			&r.WriteLagBytes,
			&r.FlushLagBytes,
			&r.ReplayLagBytes,
			&r.ReplayLagSeconds,
		); err != nil {
			continue
		}
		result = append(result, r)
	}
	if result == nil {
		result = []model.ReplicationLag{}
	}
	return result, nil
}

func (m *Monitor) getWALStats(ctx context.Context) (model.WALStats, error) {
	var stats model.WALStats
	if err := m.pool.QueryRow(ctx, `SELECT pg_current_wal_lsn()::text`).Scan(&stats.CurrentLSN); err != nil {
		return stats, err
	}

	currentLSN, err := parseLSN(stats.CurrentLSN)
	if err != nil {
		return stats, err
	}

	m.walMu.Lock()
	defer m.walMu.Unlock()
	now := time.Now()
	if !m.prevWalAt.IsZero() && currentLSN >= m.prevWalLSN {
		seconds := now.Sub(m.prevWalAt).Seconds()
		if seconds > 0 {
			stats.BytesPerSec = float64(currentLSN-m.prevWalLSN) / seconds
			stats.SegmentsPerHour = stats.BytesPerSec * 3600 / (16 * 1024 * 1024)
		}
	}
	m.prevWalLSN = currentLSN
	m.prevWalAt = now
	return stats, nil
}

func (m *Monitor) getBGWriterStats(ctx context.Context) (model.BGWriterStats, error) {
	var stats model.BGWriterStats
	if checkpointer, err := m.readStatsView(ctx, "pg_stat_checkpointer"); err == nil {
		stats.CheckpointsTimed = jsonInt64(checkpointer, "num_timed", "checkpoints_timed")
		stats.CheckpointsRequested = jsonInt64(checkpointer, "num_requested", "checkpoints_req")
		stats.CheckpointWriteMs = jsonFloat64(checkpointer, "write_time", "checkpoint_write_time")
		stats.CheckpointSyncMs = jsonFloat64(checkpointer, "sync_time", "checkpoint_sync_time")
		stats.BuffersCheckpoint = jsonInt64(checkpointer, "buffers_written", "buffers_checkpoint")
	}

	bgwriter, err := m.readStatsView(ctx, "pg_stat_bgwriter")
	if err != nil {
		return stats, err
	}
	stats.BuffersClean = jsonInt64(bgwriter, "buffers_clean")
	stats.MaxwrittenClean = jsonInt64(bgwriter, "maxwritten_clean")
	stats.BuffersBackend = jsonInt64(bgwriter, "buffers_backend")
	if stats.CheckpointsTimed == 0 && stats.CheckpointsRequested == 0 && stats.BuffersCheckpoint == 0 {
		stats.CheckpointsTimed = jsonInt64(bgwriter, "checkpoints_timed", "num_timed")
		stats.CheckpointsRequested = jsonInt64(bgwriter, "checkpoints_req", "num_requested")
		stats.CheckpointWriteMs = jsonFloat64(bgwriter, "checkpoint_write_time", "write_time")
		stats.CheckpointSyncMs = jsonFloat64(bgwriter, "checkpoint_sync_time", "sync_time")
		stats.BuffersCheckpoint = jsonInt64(bgwriter, "buffers_checkpoint", "buffers_written")
	}
	return stats, nil
}

func (m *Monitor) getSystemStats() model.SystemStats {
	m.systemMu.RLock()
	if !m.lastSystemAt.IsZero() {
		stats := m.lastSystem
		m.systemMu.RUnlock()
		return stats
	}
	m.systemMu.RUnlock()

	m.refreshSystemStats()

	m.systemMu.RLock()
	defer m.systemMu.RUnlock()
	return m.lastSystem
}

func (m *Monitor) refreshSystemStats() {
	m.systemMu.Lock()
	defer m.systemMu.Unlock()
	m.lastSystem = m.collectSystemStatsLocked()
	m.lastSystemAt = time.Now()
}

func (m *Monitor) collectSystemStatsLocked() model.SystemStats {
	var s model.SystemStats

	// CPU
	cur, err := readCPUTimes()
	if err == nil {
		totalDelta := float64(cur.total - m.prevCPU.total)
		idleDelta := float64(cur.idleTotal - m.prevCPU.idleTotal)
		if totalDelta > 0 {
			s.CPUUsage = (1 - idleDelta/totalDelta) * 100
		}
		m.prevCPU = cur
	}

	// Memory
	if f, err := os.Open("/proc/meminfo"); err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		mem := map[string]uint64{}
		for scanner.Scan() {
			parts := strings.Fields(scanner.Text())
			if len(parts) >= 2 {
				key := strings.TrimSuffix(parts[0], ":")
				val, _ := strconv.ParseUint(parts[1], 10, 64)
				mem[key] = val * 1024 // kB to bytes
			}
		}
		s.MemTotal = mem["MemTotal"]
		s.MemFree = mem["MemAvailable"]
		if s.MemFree == 0 {
			s.MemFree = mem["MemFree"]
		}
		s.MemUsed = s.MemTotal - s.MemFree
		if s.MemTotal > 0 {
			s.MemUsage = float64(s.MemUsed) / float64(s.MemTotal) * 100
		}
	}

	// Disk
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/bitnami/postgresql/data", &stat); err == nil {
		s.DiskTotal = stat.Blocks * uint64(stat.Bsize)
		s.DiskFree = stat.Bavail * uint64(stat.Bsize)
		s.DiskUsed = s.DiskTotal - s.DiskFree
		if s.DiskTotal > 0 {
			s.DiskUsage = float64(s.DiskUsed) / float64(s.DiskTotal) * 100
		}
	}

	// Load average
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		parts := strings.Fields(string(data))
		if len(parts) >= 3 {
			s.LoadAvg1, _ = strconv.ParseFloat(parts[0], 64)
			s.LoadAvg5, _ = strconv.ParseFloat(parts[1], 64)
			s.LoadAvg15, _ = strconv.ParseFloat(parts[2], 64)
		}
	}

	// Uptime
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		parts := strings.Fields(string(data))
		if len(parts) >= 1 {
			sec, _ := strconv.ParseFloat(parts[0], 64)
			hours := int(sec) / 3600
			mins := (int(sec) % 3600) / 60
			s.Uptime = fmt.Sprintf("%dd %dh %dm", hours/24, hours%24, mins)
		}
	}

	return s
}

func readCPUTimes() (cpuTimes, error) {
	var t cpuTimes
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return t, err
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) >= 8 {
				t.user, _ = strconv.ParseUint(fields[1], 10, 64)
				t.nice, _ = strconv.ParseUint(fields[2], 10, 64)
				t.system, _ = strconv.ParseUint(fields[3], 10, 64)
				t.idle, _ = strconv.ParseUint(fields[4], 10, 64)
				t.iowait, _ = strconv.ParseUint(fields[5], 10, 64)
				t.irq, _ = strconv.ParseUint(fields[6], 10, 64)
				t.softirq, _ = strconv.ParseUint(fields[7], 10, 64)
				if len(fields) >= 9 {
					t.steal, _ = strconv.ParseUint(fields[8], 10, 64)
				}
				t.idleTotal = t.idle + t.iowait
				t.total = t.user + t.nice + t.system + t.idle + t.iowait + t.irq + t.softirq + t.steal
			}
			break
		}
	}
	return t, nil
}

func parseLSN(lsn string) (uint64, error) {
	parts := strings.Split(strings.TrimSpace(lsn), "/")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid LSN: %s", lsn)
	}
	hi, err := strconv.ParseUint(parts[0], 16, 32)
	if err != nil {
		return 0, err
	}
	lo, err := strconv.ParseUint(parts[1], 16, 32)
	if err != nil {
		return 0, err
	}
	return (hi << 32) + lo, nil
}

func (m *Monitor) readStatsView(ctx context.Context, view string) (map[string]any, error) {
	var raw []byte
	query := fmt.Sprintf("SELECT row_to_json(t) FROM %s t", view)
	if err := m.pool.QueryRow(ctx, query).Scan(&raw); err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func jsonInt64(values map[string]any, keys ...string) int64 {
	for _, key := range keys {
		v, ok := values[key]
		if !ok || v == nil {
			continue
		}
		switch n := v.(type) {
		case float64:
			return int64(n)
		case int64:
			return n
		case string:
			parsed, err := strconv.ParseInt(n, 10, 64)
			if err == nil {
				return parsed
			}
		}
	}
	return 0
}

func jsonFloat64(values map[string]any, keys ...string) float64 {
	for _, key := range keys {
		v, ok := values[key]
		if !ok || v == nil {
			continue
		}
		switch n := v.(type) {
		case float64:
			return n
		case int64:
			return float64(n)
		case string:
			parsed, err := strconv.ParseFloat(n, 64)
			if err == nil {
				return parsed
			}
		}
	}
	return 0
}

// CancelQuery cancels a backend by PID.
func (m *Monitor) CancelQuery(ctx context.Context, pid int) error {
	var ok bool
	if err := m.pool.QueryRow(ctx, "SELECT pg_cancel_backend($1)", pid).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("backend %d could not be cancelled", pid)
	}
	return nil
}

// TerminateBackend terminates a backend by PID.
func (m *Monitor) TerminateBackend(ctx context.Context, pid int) error {
	var ok bool
	if err := m.pool.QueryRow(ctx, "SELECT pg_terminate_backend($1)", pid).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("backend %d could not be terminated", pid)
	}
	return nil
}
