package service

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"pgaio/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Monitor collects PostgreSQL and system statistics.
type Monitor struct {
	pool    *pgxpool.Pool
	prevCPU cpuTimes
}

type cpuTimes struct {
	user, nice, system, idle, iowait, irq, softirq, steal uint64
	total, idleTotal                                      uint64
}

func NewMonitor(pool *pgxpool.Pool) *Monitor {
	m := &Monitor{pool: pool}
	m.prevCPU, _ = readCPUTimes()
	return m
}

// CollectStats gathers all PostgreSQL and system statistics.
func (m *Monitor) CollectStats(ctx context.Context) (*model.PgStat, error) {
	stat := &model.PgStat{Timestamp: time.Now()}

	// Database stats
	db, err := m.getDatabaseStats(ctx)
	if err == nil {
		stat.Database = db
	}

	// Activity
	activity, err := m.getActivityStats(ctx)
	if err == nil {
		stat.Activity = activity
	}

	// Connections
	conns, err := m.getConnectionStats(ctx)
	if err == nil {
		stat.Connections = conns
	}

	// Replication
	repl, _ := m.getReplicationStats(ctx)
	stat.Replication = repl

	// System
	stat.System = m.getSystemStats()

	return stat, nil
}

func (m *Monitor) getDatabaseStats(ctx context.Context) (model.DatabaseStats, error) {
	var s model.DatabaseStats
	err := m.pool.QueryRow(ctx, `
		SELECT
			d.datname,
			pg_size_pretty(pg_database_size(d.datname)),
			COALESCE(s.xact_commit, 0),
			COALESCE(s.xact_rollback, 0),
			COALESCE(s.blks_read, 0),
			COALESCE(s.blks_hit, 0),
			CASE WHEN COALESCE(s.blks_hit, 0) + COALESCE(s.blks_read, 0) = 0 THEN 0
				ELSE round(COALESCE(s.blks_hit, 0)::numeric / (COALESCE(s.blks_hit, 0) + COALESCE(s.blks_read, 0))::numeric * 100, 2)
			END,
			COALESCE(s.temp_files, 0),
			COALESCE(s.temp_bytes, 0),
			COALESCE(s.deadlocks, 0),
			COALESCE(s.conflicts, 0),
			COALESCE(s.tup_returned, 0),
			COALESCE(s.tup_fetched, 0),
			COALESCE(s.tup_inserted, 0),
			COALESCE(s.tup_updated, 0),
			COALESCE(s.tup_deleted, 0)
		FROM pg_database d
		LEFT JOIN pg_stat_database s ON s.datname = d.datname
		WHERE d.datname = current_database()
	`).Scan(
		&s.Name, &s.Size,
		&s.TxCommit, &s.TxRollback,
		&s.BlksRead, &s.BlksHit, &s.CacheHitRatio,
		&s.TempFiles, &s.TempBytes,
		&s.Deadlocks, &s.Conflicts,
		&s.TupReturned, &s.TupFetched,
		&s.TupInserted, &s.TupUpdated, &s.TupDeleted,
	)
	return s, err
}

func (m *Monitor) getActivityStats(ctx context.Context) (model.ActivityStats, error) {
	var a model.ActivityStats

	// Counts
	err := m.pool.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE state = 'active'),
			count(*) FILTER (WHERE state = 'idle'),
			count(*) FILTER (WHERE wait_event IS NOT NULL AND state = 'active')
		FROM pg_stat_activity
		WHERE backend_type = 'client backend'
	`).Scan(&a.TotalConnections, &a.ActiveQueries, &a.IdleConnections, &a.WaitingQueries)
	if err != nil {
		return a, err
	}

	// Active queries detail
	rows, err := m.pool.Query(ctx, `
		SELECT
			pid,
			COALESCE(usename, ''),
			COALESCE(datname, ''),
			COALESCE(state, 'unknown'),
			COALESCE(LEFT(query, 200), ''),
			COALESCE(age(clock_timestamp(), query_start)::text, ''),
			COALESCE(wait_event, ''),
			COALESCE(backend_type, ''),
			COALESCE(query_start, now())
		FROM pg_stat_activity
		WHERE backend_type = 'client backend' AND state != 'idle' AND pid != pg_backend_pid()
		ORDER BY query_start ASC
		LIMIT 50
	`)
	if err != nil {
		return a, err
	}
	defer rows.Close()

	for rows.Next() {
		var q model.ActiveQuery
		if err := rows.Scan(&q.PID, &q.User, &q.Database, &q.State, &q.Query, &q.Duration, &q.WaitEvent, &q.BackendType, &q.QueryStart); err != nil {
			continue
		}
		a.Queries = append(a.Queries, q)
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
			setting::int - (SELECT count(*) FROM pg_stat_activity) AS available,
			(SELECT setting::int FROM pg_settings WHERE name = 'superuser_reserved_connections') AS reserved
		FROM pg_settings
		WHERE name = 'max_connections'
	`).Scan(&c.MaxConnections, &c.UsedConnections, &c.AvailableConnections, &c.ReservedConnections)
	return c, err
}

func (m *Monitor) getReplicationStats(ctx context.Context) ([]model.ReplicationLag, error) {
	rows, err := m.pool.Query(ctx, `
		SELECT
			COALESCE(client_addr::text, 'local'),
			COALESCE(state, ''),
			COALESCE(sent_lsn::text, ''),
			COALESCE(write_lsn::text, ''),
			COALESCE(flush_lsn::text, ''),
			COALESCE(replay_lsn::text, '')
		FROM pg_stat_replication
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []model.ReplicationLag
	for rows.Next() {
		var r model.ReplicationLag
		if err := rows.Scan(&r.ClientAddr, &r.State, &r.SentLag, &r.WriteLag, &r.FlushLag, &r.ReplayLag); err != nil {
			continue
		}
		result = append(result, r)
	}
	if result == nil {
		result = []model.ReplicationLag{}
	}
	return result, nil
}

func (m *Monitor) getSystemStats() model.SystemStats {
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

// CancelQuery cancels a backend by PID.
func (m *Monitor) CancelQuery(ctx context.Context, pid int) error {
	_, err := m.pool.Exec(ctx, "SELECT pg_cancel_backend($1)", pid)
	return err
}

// TerminateBackend terminates a backend by PID.
func (m *Monitor) TerminateBackend(ctx context.Context, pid int) error {
	_, err := m.pool.Exec(ctx, "SELECT pg_terminate_backend($1)", pid)
	return err
}
