package service

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	"pgaio/model"

	"github.com/jackc/pgx/v5"
)

// PgBouncer manages PgBouncer admin operations via its admin console.
type PgBouncer struct {
	adminAddr string
	adminUser string
	adminPass string
}

func NewPgBouncer(adminAddr, adminUser, adminPass string) *PgBouncer {
	if adminAddr == "" {
		adminAddr = "127.0.0.1:6432"
	}
	if adminUser == "" {
		adminUser = "pgbouncer"
	}
	return &PgBouncer{
		adminAddr: adminAddr,
		adminUser: adminUser,
		adminPass: adminPass,
	}
}

func (p *PgBouncer) connect(ctx context.Context) (*pgx.Conn, error) {
	host, port, _ := net.SplitHostPort(p.adminAddr)
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=pgbouncer sslmode=disable",
		host, port, p.adminUser, p.adminPass)

	config, err := pgx.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pgbouncer config: %w", err)
	}
	// PgBouncer admin doesn't support extended protocol
	config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to pgbouncer admin: %w", err)
	}
	return conn, nil
}

// GetFullStats returns stats, pools, and clients from PgBouncer.
func (p *PgBouncer) GetFullStats(ctx context.Context) (*model.PgBouncerFullStats, error) {
	conn, err := p.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)

	result := &model.PgBouncerFullStats{
		Config: map[string]string{},
	}

	// SHOW STATS
	stats, err := p.queryStats(ctx, conn)
	if err == nil {
		result.Stats = stats
	}

	// SHOW POOLS
	pools, err := p.queryPools(ctx, conn)
	if err == nil {
		result.Pools = pools
	}

	// SHOW CLIENTS
	clients, err := p.queryClients(ctx, conn)
	if err == nil {
		result.Clients = clients
	}

	return result, nil
}

func (p *PgBouncer) queryStats(ctx context.Context, conn *pgx.Conn) ([]model.PgBouncerStat, error) {
	rows, err := conn.Query(ctx, "SHOW STATS")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []model.PgBouncerStat
	for rows.Next() {
		var s model.PgBouncerStat
		vals, err := rows.Values()
		if err != nil {
			continue
		}
		// SHOW STATS returns: database, total_xact_count, total_query_count, total_received, total_sent,
		// total_xact_time, total_query_time, total_wait_time, avg_xact_count, avg_query_count, avg_recv, avg_sent,
		// avg_xact_time, avg_query_time, avg_wait_time
		if len(vals) >= 15 {
			s.Database = fmt.Sprint(vals[0])
			s.TotalXactCount, _ = toInt64(vals[1])
			s.TotalQueryCount, _ = toInt64(vals[2])
			s.TotalReceived, _ = toInt64(vals[3])
			s.TotalSent, _ = toInt64(vals[4])
			s.TotalXactTime, _ = toInt64(vals[5])
			s.TotalQueryTime, _ = toInt64(vals[6])
			s.TotalWaitTime, _ = toInt64(vals[7])
			s.AvgXactCount, _ = toInt64(vals[8])
			s.AvgQueryCount, _ = toInt64(vals[9])
			s.AvgXactTime, _ = toInt64(vals[12])
			s.AvgQueryTime, _ = toInt64(vals[13])
			s.AvgWaitTime, _ = toInt64(vals[14])
		}
		stats = append(stats, s)
	}
	if stats == nil {
		stats = []model.PgBouncerStat{}
	}
	return stats, nil
}

func (p *PgBouncer) queryPools(ctx context.Context, conn *pgx.Conn) ([]model.PgBouncerPool, error) {
	rows, err := conn.Query(ctx, "SHOW POOLS")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pools []model.PgBouncerPool
	for rows.Next() {
		var pool model.PgBouncerPool
		vals, err := rows.Values()
		if err != nil {
			continue
		}
		// database, user, cl_active, cl_waiting, cl_cancel_req, sv_active, sv_idle, sv_used, sv_tested, sv_login, maxwait, maxwait_us, pool_mode
		if len(vals) >= 13 {
			pool.Database = fmt.Sprint(vals[0])
			pool.User = fmt.Sprint(vals[1])
			pool.ClActive, _ = toInt(vals[2])
			pool.ClWaiting, _ = toInt(vals[3])
			pool.SvActive, _ = toInt(vals[5])
			pool.SvIdle, _ = toInt(vals[6])
			pool.SvUsed, _ = toInt(vals[7])
			pool.SvTested, _ = toInt(vals[8])
			pool.SvLogin, _ = toInt(vals[9])
			pool.MaxWait, _ = toInt(vals[10])
			pool.PoolMode = fmt.Sprint(vals[12])
		}
		pools = append(pools, pool)
	}
	if pools == nil {
		pools = []model.PgBouncerPool{}
	}
	return pools, nil
}

func (p *PgBouncer) queryClients(ctx context.Context, conn *pgx.Conn) ([]model.PgBouncerClient, error) {
	rows, err := conn.Query(ctx, "SHOW CLIENTS")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clients []model.PgBouncerClient
	for rows.Next() {
		var c model.PgBouncerClient
		vals, err := rows.Values()
		if err != nil {
			continue
		}
		// type, user, database, state, addr, port, local_addr, local_port, connect_time, request_time, ...
		if len(vals) >= 10 {
			c.Type = fmt.Sprint(vals[0])
			c.User = fmt.Sprint(vals[1])
			c.Database = fmt.Sprint(vals[2])
			c.State = fmt.Sprint(vals[3])
			c.Addr = fmt.Sprint(vals[4])
			c.Port, _ = toInt(vals[5])
			c.LocalAddr = fmt.Sprint(vals[6])
			c.LocalPort, _ = toInt(vals[7])
			c.ConnectTime = fmt.Sprint(vals[8])
			c.RequestTime = fmt.Sprint(vals[9])
		}
		clients = append(clients, c)
	}
	if clients == nil {
		clients = []model.PgBouncerClient{}
	}
	return clients, nil
}

// Pause pauses a database pool.
func (p *PgBouncer) Pause(ctx context.Context, database string) error {
	conn, err := p.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	q := "PAUSE"
	if database != "" {
		q += " " + database
	}
	_, err = conn.Exec(ctx, q)
	return err
}

// Resume resumes a database pool.
func (p *PgBouncer) Resume(ctx context.Context, database string) error {
	conn, err := p.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	q := "RESUME"
	if database != "" {
		q += " " + database
	}
	_, err = conn.Exec(ctx, q)
	return err
}

// Reload tells PgBouncer to reload config.
func (p *PgBouncer) Reload(ctx context.Context) error {
	conn, err := p.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, "RELOAD")
	return err
}

// Kill kills all connections for a database.
func (p *PgBouncer) Kill(ctx context.Context, database string) error {
	conn, err := p.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if database == "" {
		return fmt.Errorf("database name required for kill")
	}
	_, err = conn.Exec(ctx, "KILL "+database)
	return err
}

// UpdateConfig updates PgBouncer config file settings and reloads.
func (p *PgBouncer) UpdateConfig(ctx context.Context, settings map[string]string) error {
	configPath := "/etc/pgbouncer/pgbouncer.ini"

	// Read current config
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read pgbouncer config: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	applied := map[string]bool{}

	// Update existing settings
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip comments and empty lines
		if trimmed == "" || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "[") {
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		if newVal, ok := settings[key]; ok {
			lines[i] = fmt.Sprintf("%s = %s", key, newVal)
			applied[key] = true
		}
	}

	// Write back
	if err := os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		return fmt.Errorf("failed to write pgbouncer config: %w", err)
	}

	// Reload PgBouncer
	if err := p.Reload(ctx); err != nil {
		return fmt.Errorf("config written but reload failed: %w", err)
	}

	return nil
}

func toInt64(v any) (int64, error) {
	switch val := v.(type) {
	case int64:
		return val, nil
	case int32:
		return int64(val), nil
	case int:
		return int64(val), nil
	case float64:
		return int64(val), nil
	case string:
		if strings.TrimSpace(val) == "" {
			return 0, nil
		}
		var n int64
		_, err := fmt.Sscan(val, &n)
		return n, err
	default:
		return 0, fmt.Errorf("cannot convert %T to int64", v)
	}
}

func toInt(v any) (int, error) {
	n, err := toInt64(v)
	return int(n), err
}
