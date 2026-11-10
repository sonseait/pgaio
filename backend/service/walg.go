package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"pgaio/model"
)

// WalG wraps wal-g CLI operations.
type WalG struct {
	dataDir string
	jobs    *JobStore
	config  *ConfigStore

	// opMu serializes destructive cluster operations (backup/restore) so that
	// two restores — or a restore racing a backup — can never run at once.
	opMu sync.Mutex
	// Keep startup options across a failed recovery so it can be retried via
	// the still-running API even when PostgreSQL is no longer accepting SQL.
	pgOptions string
	pgLogPath string
}

// setPgEnv sets PostgreSQL connection env vars on a command.
func setPgEnv(cmd *exec.Cmd) {
	cmd.Env = append(os.Environ(),
		"PGUSER="+getEnvOrDefault("POSTGRESQL_USERNAME", "postgres"),
		"PGPASSWORD="+getEnvOrDefault("POSTGRESQL_PASSWORD", ""),
		"PGDATABASE="+getEnvOrDefault("POSTGRESQL_DATABASE", "postgres"),
		"PGHOST="+getEnvOrDefault("PGHOST", "/tmp"),
	)
}

// setWalGDeltaLimit prevents WAL-G from promoting scheduled deltas to full
// backups before the separately configured full-backup schedule runs.
func setWalGDeltaLimit(cmd *exec.Cmd) {
	cmd.Env = append(cmd.Env, "WALG_DELTA_MAX_STEPS=1000")
}

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func NewWalG(dataDir string, jobs *JobStore, config *ConfigStore) *WalG {
	if dataDir == "" {
		dataDir = "/bitnami/postgresql/data"
	}
	return &WalG{dataDir: dataDir, jobs: jobs, config: config}
}

// RawBackup is the struct wal-g backup-list --json outputs.
type RawBackup struct {
	BackupName       string      `json:"backup_name"`
	Time             string      `json:"time"`
	FinishTime       string      `json:"finish_time"`
	DateFmt          string      `json:"date_fmt"`
	WalFileName      string      `json:"wal_file_name"`
	StartLSN         json.Number `json:"start_lsn"`
	FinishLSN        json.Number `json:"finish_lsn"`
	Hostname         string      `json:"hostname"`
	DataDir          string      `json:"data_dir"`
	PgVersion        int         `json:"pg_version"`
	IsPermanent      bool        `json:"is_permanent"`
	SystemIdentifier json.Number `json:"system_identifier,omitempty"`
	CompressedSize   int64       `json:"compressed_size"`
	UncompressedSize int64       `json:"uncompressed_size"`
}

// ListBackups returns the list of wal-g backups.
func (w *WalG) ListBackups(ctx context.Context) (*model.BackupListResponse, error) {
	cmd := exec.CommandContext(ctx, "wal-g", "backup-list", "--json", "--detail")
	setPgEnv(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("wal-g backup-list failed: %s: %w", stderr.String(), err)
	}

	output := strings.TrimSpace(stdout.String())
	if output == "" || output == "null" {
		return &model.BackupListResponse{
			Backups:     []model.Backup{},
			BackupCount: 0,
			TotalSize:   "0 B",
		}, nil
	}

	var raw []RawBackup
	if err := json.Unmarshal([]byte(output), &raw); err != nil {
		return nil, fmt.Errorf("failed to parse backup list: %w (output: %s)", err, output)
	}

	var backups []model.Backup
	var totalSize int64
	for _, r := range raw {
		startTime, _ := parseWalgTime(r.Time)
		finishTime, _ := parseWalgTime(r.FinishTime)

		backups = append(backups, model.Backup{
			Name:             r.BackupName,
			StartTime:        startTime,
			FinishTime:       finishTime,
			StartLSN:         r.StartLSN.String(),
			FinishLSN:        r.FinishLSN.String(),
			Hostname:         r.Hostname,
			DataDir:          r.DataDir,
			CompressedSize:   r.CompressedSize,
			UncompressedSize: r.UncompressedSize,
			IsPermanent:      r.IsPermanent,
			WalFileName:      r.WalFileName,
		})
		totalSize += r.CompressedSize
	}

	resp := &model.BackupListResponse{
		Backups:     backups,
		BackupCount: len(backups),
		TotalSize:   humanBytes(totalSize),
	}
	if name := latestByTime(backups); name != "" {
		resp.LastBackup = name
	}
	return resp, nil
}

// latestByTime returns the name of the most recent backup by finish time
// (falling back to start time), independent of the order wal-g returns them in.
func latestByTime(backups []model.Backup) string {
	var (
		latestName string
		latestTime time.Time
	)
	for _, b := range backups {
		candidate := b.FinishTime
		if candidate.IsZero() {
			candidate = b.StartTime
		}
		if latestName == "" || candidate.After(latestTime) {
			latestName = b.Name
			latestTime = candidate
		}
	}
	return latestName
}

func (w *WalG) LatestBackupTime(ctx context.Context) (time.Time, error) {
	resp, err := w.ListBackups(ctx)
	if err != nil {
		return time.Time{}, err
	}
	var latest time.Time
	for _, backup := range resp.Backups {
		candidate := backup.FinishTime
		if candidate.IsZero() {
			candidate = backup.StartTime
		}
		if candidate.After(latest) {
			latest = candidate
		}
	}
	return latest, nil
}

func (w *WalG) latestBackupName(ctx context.Context) (string, error) {
	resp, err := w.ListBackups(ctx)
	if err != nil {
		return "", err
	}
	if len(resp.Backups) == 0 {
		return "", fmt.Errorf("no backups found")
	}
	return latestByTime(resp.Backups), nil
}

func parseWalgTime(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.999999+00:00",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse time: %s", s)
}

// TriggerBackup starts a manual incremental backup.
func (w *WalG) TriggerBackup(ctx context.Context) (*model.BackupTriggerResponse, error) {
	return w.triggerBackup(ctx, "incremental")
}

// TriggerFullBackup starts a full backup, independent of the existing chain.
func (w *WalG) TriggerFullBackup(ctx context.Context) (*model.BackupTriggerResponse, error) {
	return w.triggerBackup(ctx, "full")
}

// TriggerIncrementalBackup starts a delta backup based on the newest backup.
// It is skipped until a scheduled or manually requested full backup exists.
func (w *WalG) TriggerIncrementalBackup(ctx context.Context) (*model.BackupTriggerResponse, error) {
	return w.triggerBackup(ctx, "incremental")
}

func (w *WalG) triggerBackup(_ context.Context, kind string) (*model.BackupTriggerResponse, error) {
	job := w.jobs.Start("backup", "wal-g backup-push", "", kind+" backup queued", map[string]string{"kind": kind})
	go func() {
		// Serialize against restores and other backups — concurrent
		// backup-push / restore would corrupt the cluster or the WAL stream.
		w.opMu.Lock()
		defer w.opMu.Unlock()

		args := []string{"backup-push", w.dataDir}
		actualKind := kind
		if kind == "full" {
			args = append(args, "--full")
		} else {
			backups, err := w.ListBackups(context.Background())
			if err != nil {
				w.jobs.Fail(job.ID, "incremental backup could not inspect base backup", err.Error())
				return
			}
			if len(backups.Backups) == 0 {
				w.jobs.Cancel(job.ID, "incremental backup skipped: no full base backup yet")
				return
			}
			args = append(args, "--delta-from-name", latestByTime(backups.Backups))
		}
		w.jobs.Update(job.ID, "running WAL-G "+actualKind+" backup", "")
		log.Printf("[walg] starting %s backup...", actualKind)
		cmd := exec.Command("wal-g", args...)
		setPgEnv(cmd)
		if actualKind == "incremental" {
			setWalGDeltaLimit(cmd)
		}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			w.jobs.Fail(job.ID, "backup failed", stderr.String())
			log.Printf("[walg] %s backup failed: %s: %v", actualKind, stderr.String(), err)
			return
		}
		log.Printf("[walg] %s backup completed successfully", actualKind)

		// Enforce retention so S3 storage does not grow unbounded.
		if msg := w.applyRetention(); msg != "" {
			w.jobs.Complete(job.ID, actualKind+" backup completed successfully; "+msg)
			return
		}
		w.jobs.Complete(job.ID, actualKind+" backup completed successfully")
	}()

	return &model.BackupTriggerResponse{
		Message: kind + " backup triggered in background",
		Status:  "running",
		JobID:   job.ID,
	}, nil
}

// applyRetention deletes old backups beyond the configured RetainCount using
// `wal-g delete retain FULL <n> --confirm`. It returns a short human-readable
// status suffix (empty when retention is disabled or nothing needed pruning).
// Retention failures are logged but never fail the backup job itself — the
// backup already succeeded, and a failed prune should not mask that.
func (w *WalG) applyRetention() string {
	if w.config == nil {
		return ""
	}
	retain := w.config.GetBackup().RetainCount
	if retain <= 0 {
		log.Println("[walg] retention disabled (retainCount <= 0), skipping prune")
		return ""
	}

	log.Printf("[walg] pruning backups, retaining last %d full backups...", retain)
	cmd := exec.Command("wal-g", "delete", "retain", "FULL", strconv.Itoa(retain), "--confirm")
	setPgEnv(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		log.Printf("[walg] retention prune failed: %s: %v", stderr.String(), err)
		return "retention prune failed (see logs)"
	}
	log.Printf("[walg] retention prune complete (kept %d full backups)", retain)
	return fmt.Sprintf("pruned to last %d backups", retain)
}

func (w *WalG) VerifyBackup(ctx context.Context, backupName string) (*model.BackupTriggerResponse, error) {
	name := strings.TrimSpace(backupName)
	if name == "" || strings.EqualFold(name, "LATEST") {
		var err error
		name, err = w.latestBackupName(ctx)
		if err != nil {
			return nil, err
		}
	}

	job := w.jobs.Start("backup_verify", name, "", "verification queued", map[string]string{
		"backupName": name,
	})

	go func() {
		tmpDir, err := os.MkdirTemp("", "pgaio-verify-*")
		if err != nil {
			w.jobs.Fail(job.ID, "verification failed", err.Error())
			return
		}
		defer os.RemoveAll(tmpDir)

		restoreDir := filepath.Join(tmpDir, "restore")
		if err := os.MkdirAll(restoreDir, 0o755); err != nil {
			w.jobs.Fail(job.ID, "verification failed", err.Error())
			return
		}

		w.jobs.Update(job.ID, "fetching backup into scratch directory", "")
		fetchCmd := exec.Command("wal-g", "backup-fetch", restoreDir, name)
		setPgEnv(fetchCmd)
		if out, err := fetchCmd.CombinedOutput(); err != nil {
			w.jobs.Fail(job.ID, "verification failed while fetching backup", string(out))
			return
		}

		pgVersionPath := filepath.Join(restoreDir, "PG_VERSION")
		pgControlPath := filepath.Join(restoreDir, "global", "pg_control")
		baseDir := filepath.Join(restoreDir, "base")
		pgVersion, err := os.ReadFile(pgVersionPath)
		if err != nil {
			w.jobs.Fail(job.ID, "verification failed", "missing PG_VERSION in fetched backup")
			return
		}
		controlInfo, err := os.Stat(pgControlPath)
		if err != nil {
			w.jobs.Fail(job.ID, "verification failed", "missing global/pg_control in fetched backup")
			return
		}
		baseEntries, err := os.ReadDir(baseDir)
		if err != nil {
			w.jobs.Fail(job.ID, "verification failed", "missing base directory in fetched backup")
			return
		}

		report := strings.Builder{}
		report.WriteString("PGAIO backup verification report\n")
		report.WriteString("================================\n")
		report.WriteString(fmt.Sprintf("backup: %s\n", name))
		report.WriteString(fmt.Sprintf("verified_at: %s\n", time.Now().Format(time.RFC3339)))
		report.WriteString(fmt.Sprintf("pg_version: %s\n", strings.TrimSpace(string(pgVersion))))
		report.WriteString(fmt.Sprintf("base_directories: %d\n", len(baseEntries)))
		report.WriteString(fmt.Sprintf("pg_control_size_bytes: %d\n", controlInfo.Size()))

		reportFile, err := os.CreateTemp("", fmt.Sprintf("pgaio-verify-%s-*.txt", sanitizeArtifactName(name)))
		if err != nil {
			w.jobs.Fail(job.ID, "verification failed", err.Error())
			return
		}
		reportPath := reportFile.Name()
		if _, err := reportFile.WriteString(report.String()); err != nil {
			reportFile.Close()
			w.jobs.Fail(job.ID, "verification failed", err.Error())
			return
		}
		if err := reportFile.Close(); err != nil {
			w.jobs.Fail(job.ID, "verification failed", err.Error())
			return
		}

		info, err := os.Stat(reportPath)
		if err != nil {
			w.jobs.Fail(job.ID, "verification failed", err.Error())
			return
		}

		w.jobs.CompleteWithArtifact(job.ID, "backup verification completed", &JobArtifact{
			Path:        reportPath,
			Name:        filepath.Base(reportPath),
			ContentType: "text/plain; charset=utf-8",
			SizeBytes:   info.Size(),
		})
	}()

	return &model.BackupTriggerResponse{
		Message: "Backup verification started in background",
		Status:  "running",
		JobID:   job.ID,
	}, nil
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func sanitizeArtifactName(name string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", " ", "_", ":", "_")
	return replacer.Replace(name)
}
