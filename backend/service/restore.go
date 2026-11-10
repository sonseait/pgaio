package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"pgaio/model"

	"github.com/jackc/pgx/v5"
)

// selectRestoreBackup resolves LATEST before stopping PostgreSQL. PITR needs a
// completed backup at or before the target; it cannot replay WAL backwards.
func selectRestoreBackup(backups []model.Backup, name string, target time.Time) (string, error) {
	var eligible []model.Backup
	for _, backup := range backups {
		if !strings.EqualFold(name, "LATEST") && backup.Name != name {
			continue
		}
		if !target.IsZero() && (backup.FinishTime.IsZero() || backup.FinishTime.After(target) || backup.StartTime.After(target)) {
			continue
		}
		eligible = append(eligible, backup)
	}
	if len(eligible) == 0 {
		return "", fmt.Errorf("no completed backup matching %q is available before the recovery target", name)
	}
	return latestByTime(eligible), nil
}

// RestoreBackup fetches and configures a backup before stopping the live
// cluster. Without targetTime, recover to the selected backup's first
// consistent point. With targetTime, replay its timeline to that instant.
func (w *WalG) RestoreBackup(ctx context.Context, req model.RestoreRequest) (*model.BackupTriggerResponse, error) {
	req.BackupName = strings.TrimSpace(req.BackupName)
	if req.BackupName == "" {
		return nil, fmt.Errorf("backupName is required")
	}
	var target time.Time
	if req.TargetTime != "" {
		var err error
		target, err = parseWalgTime(req.TargetTime)
		if err != nil {
			return nil, fmt.Errorf("invalid targetTime: expected an RFC3339 timestamp")
		}
		if target.After(time.Now()) {
			return nil, fmt.Errorf("targetTime must not be in the future")
		}
	}
	backups, err := w.ListBackups(ctx)
	if err != nil {
		return nil, err
	}
	name, err := selectRestoreBackup(backups.Backups, req.BackupName, target)
	if err != nil {
		return nil, err
	}
	job := w.jobs.Start("restore", name, "", "restore queued", map[string]string{
		"backupName": name,
		"targetTime": req.TargetTime,
	})
	go func() {
		w.opMu.Lock()
		defer w.opMu.Unlock()
		if err := w.restore(job.ID, name, target); err != nil {
			w.jobs.Fail(job.ID, "restore failed", err.Error())
			log.Printf("[walg] restore failed: %v", err)
			return
		}
		w.jobs.Complete(job.ID, "restore completed; PostgreSQL is ready")
	}()
	return &model.BackupTriggerResponse{
		Message: "Restore queued for " + name + ". PostgreSQL will restart.",
		Status:  "running",
		JobID:   job.ID,
	}, nil
}

func (w *WalG) restore(jobID, name string, target time.Time) error {
	dataDir := filepath.Clean(w.dataDir)
	if !filepath.IsAbs(dataDir) || dataDir == string(filepath.Separator) {
		return fmt.Errorf("refusing to restore into invalid data directory %q", w.dataDir)
	}
	// Stage inside PGDATA so renames also work when PGDATA itself is a volume
	// mount. PostgreSQL ignores this temporary subdirectory. A failed fetch
	// leaves the live cluster untouched; installation preserves the staging dir.
	stage, err := os.MkdirTemp(dataDir, ".pgaio-restore-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	w.jobs.Update(jobID, "fetching backup into staging directory", "")
	fetch := exec.Command("wal-g", "backup-fetch", stage, name)
	setPgEnv(fetch)
	if out, err := fetch.CombinedOutput(); err != nil {
		return fmt.Errorf("fetch backup: %w: %s", err, out)
	}
	if err := configureRecovery(stage, target); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	options, logPath, err := livePostgresOptions(ctx)
	if err != nil {
		if w.pgOptions == "" {
			return fmt.Errorf("read running PostgreSQL configuration: %w", err)
		}
		options, logPath = w.pgOptions, w.pgLogPath
	} else {
		w.pgOptions, w.pgLogPath = options, logPath
	}
	w.jobs.Update(jobID, "stopping PostgreSQL", "")
	status := exec.CommandContext(ctx, "pg_ctl", "status", "-D", dataDir)
	if out, err := status.CombinedOutput(); err != nil {
		// Exit 3 means the server is already stopped (for example after a
		// failed recovery). Other failures must not permit data replacement.
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 3 {
			return fmt.Errorf("inspect PostgreSQL status: %w: %s", err, out)
		}
	} else {
		stop := exec.CommandContext(ctx, "pg_ctl", "stop", "-D", dataDir, "-m", "fast", "-w", "-t", "60")
		setPgEnv(stop)
		if out, err := stop.CombinedOutput(); err != nil {
			return fmt.Errorf("stop PostgreSQL: %w: %s", err, out)
		}
	}
	w.jobs.Update(jobID, "installing fetched backup", "")
	if err := installRestore(dataDir, stage); err != nil {
		return err
	}
	w.jobs.Update(jobID, "starting PostgreSQL and waiting for recovery", "")
	start := exec.CommandContext(ctx, "pg_ctl", "start", "-D", dataDir, "-l", "/tmp/pg_restore.log", "-w", "-t", "300", "-o", options)
	setPgEnv(start)
	if out, err := start.CombinedOutput(); err != nil {
		return fmt.Errorf("start PostgreSQL: %w: %s\n%s", err, out, restoreLogs(logPath))
	}
	if err := waitForRecovery(ctx, dataDir); err != nil {
		return fmt.Errorf("complete recovery: %w\n%s", err, restoreLogs(logPath))
	}
	return nil
}

func installRestore(dataDir, stage string) error {
	if filepath.Dir(stage) != dataDir {
		return fmt.Errorf("restore staging directory must be inside the data directory")
	}
	fetched, err := os.ReadDir(stage)
	if err != nil {
		return err
	}
	existing, err := os.ReadDir(dataDir)
	if err != nil {
		return err
	}
	for _, entry := range existing {
		path := filepath.Join(dataDir, entry.Name())
		if path != stage {
			if err := os.RemoveAll(path); err != nil {
				return fmt.Errorf("remove old data: %w", err)
			}
		}
	}
	for _, entry := range fetched {
		if err := os.Rename(filepath.Join(stage, entry.Name()), filepath.Join(dataDir, entry.Name())); err != nil {
			return fmt.Errorf("install fetched backup: %w", err)
		}
	}
	return nil
}

func configureRecovery(dir string, target time.Time) error {
	path := filepath.Join(dir, "postgresql.auto.conf")
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var lines []string
	for line := range strings.SplitSeq(string(old), "\n") {
		key, _, _ := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if key == "restore_command" || key == "recovery_target" || strings.HasPrefix(key, "recovery_target_") {
			continue // Remove recovery settings inherited from a previous restore.
		}
		lines = append(lines, line)
	}
	lines = append(lines,
		`restore_command = 'wal-g wal-fetch "%f" "%p"'`,
		`recovery_target = ''`,
		`recovery_target_name = ''`,
		`recovery_target_xid = ''`,
		`recovery_target_lsn = ''`,
		`recovery_target_time = ''`,
		`recovery_target_inclusive = on`,
		`recovery_target_timeline = 'current'`,
		`recovery_target_action = 'promote'`,
	)
	if target.IsZero() {
		lines = append(lines, `recovery_target = 'immediate'`)
	} else {
		// PostgreSQL's recovery GUC does not accept the RFC3339 T/Z form.
		// Include an explicit UTC offset so server timezone cannot shift PITR.
		lines = append(lines, "recovery_target_time = '"+target.UTC().Format("2006-01-02 15:04:05.999999999-07:00")+"'")
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, "standby.signal")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "recovery.signal"), nil, 0600)
}

func localPostgres(ctx context.Context) (*pgx.Conn, error) {
	cfg, err := pgx.ParseConfig("")
	if err != nil {
		return nil, err
	}
	cfg.Host = getEnvOrDefault("PGHOST", "/tmp")
	port, err := strconv.ParseUint(getEnvOrDefault("PGPORT", "5432"), 10, 16)
	if err != nil {
		return nil, err
	}
	cfg.Port = uint16(port)
	cfg.User = getEnvOrDefault("POSTGRESQL_USERNAME", "postgres")
	cfg.Password = getEnvOrDefault("POSTGRESQL_PASSWORD", "")
	cfg.Database = getEnvOrDefault("POSTGRESQL_DATABASE", "postgres")
	cfg.ConnectTimeout = 2 * time.Second
	return pgx.ConnectConfig(ctx, cfg)
}

func livePostgresOptions(ctx context.Context) (string, string, error) {
	conn, err := localPostgres(ctx)
	if err != nil {
		return "", "", err
	}
	defer conn.Close(ctx)
	var config, hba, ident, pid, logfile string
	err = conn.QueryRow(ctx, `SELECT current_setting('config_file'), current_setting('hba_file'),
		current_setting('ident_file'), current_setting('external_pid_file'), COALESCE(pg_current_logfile(), '')`).Scan(&config, &hba, &ident, &pid, &logfile)
	if err != nil {
		return "", "", err
	}
	var options []string
	for _, setting := range []string{"config_file=" + config, "hba_file=" + hba, "ident_file=" + ident, "external_pid_file=" + pid} {
		// pg_ctl passes -o through a shell; quote each value, including paths
		// containing spaces or apostrophes, rather than concatenating raw paths.
		options = append(options, "-c", "'"+strings.ReplaceAll(setting, "'", "'\\''")+"'")
	}
	return strings.Join(options, " "), logfile, nil
}

func waitForRecovery(ctx context.Context, dataDir string) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		conn, err := localPostgres(ctx)
		if err == nil {
			var ready bool
			err = conn.QueryRow(ctx, "SELECT NOT pg_is_in_recovery()").Scan(&ready)
			conn.Close(ctx)
			if err == nil && ready {
				return nil
			}
		}
		// pg_ctl start may succeed during hot standby, before a later recovery
		// failure. Keep the job running until promotion, and detect that exit.
		status := exec.CommandContext(ctx, "pg_ctl", "status", "-D", dataDir)
		if err := status.Run(); err != nil {
			return fmt.Errorf("PostgreSQL exited before recovery completed")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func restoreLogs(logPath string) string {
	var parts []string
	for _, path := range []string{"/tmp/pg_restore.log", logPath} {
		if path == "" {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		info, err := f.Stat()
		if err == nil {
			size := min(info.Size(), 8192)
			buf := make([]byte, size)
			if n, _ := f.ReadAt(buf, info.Size()-size); n > 0 {
				parts = append(parts, path+":\n"+string(buf[:n]))
			}
		}
		f.Close()
	}
	return strings.Join(parts, "\n")
}
