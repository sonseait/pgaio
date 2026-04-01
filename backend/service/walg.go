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
	"strings"
	"time"

	"pgaio/model"
)

// WalG wraps wal-g CLI operations.
type WalG struct {
	dataDir string
	jobs    *JobStore
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

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func NewWalG(dataDir string, jobs *JobStore) *WalG {
	if dataDir == "" {
		dataDir = "/bitnami/postgresql/data"
	}
	return &WalG{dataDir: dataDir, jobs: jobs}
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
	if len(backups) > 0 {
		resp.LastBackup = backups[len(backups)-1].Name
	}
	return resp, nil
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
	latest := resp.Backups[0]
	latestTime := latest.FinishTime
	if latestTime.IsZero() {
		latestTime = latest.StartTime
	}
	for _, backup := range resp.Backups[1:] {
		candidate := backup.FinishTime
		if candidate.IsZero() {
			candidate = backup.StartTime
		}
		if candidate.After(latestTime) {
			latest = backup
			latestTime = candidate
		}
	}
	return latest.Name, nil
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

// TriggerBackup starts a manual backup.
func (w *WalG) TriggerBackup(ctx context.Context) (*model.BackupTriggerResponse, error) {
	job := w.jobs.Start("backup", "wal-g backup-push", "", "backup queued", nil)
	go func() {
		w.jobs.Update(job.ID, "running WAL-G backup", "")
		log.Println("[walg] starting manual backup...")
		cmd := exec.Command("wal-g", "backup-push", w.dataDir)
		setPgEnv(cmd)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			w.jobs.Fail(job.ID, "backup failed", stderr.String())
			log.Printf("[walg] manual backup failed: %s: %v", stderr.String(), err)
			return
		}
		w.jobs.Complete(job.ID, "backup completed successfully")
		log.Println("[walg] manual backup completed successfully")
	}()

	return &model.BackupTriggerResponse{
		Message: "Backup triggered in background",
		Status:  "running",
		JobID:   job.ID,
	}, nil
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

// RestoreBackup initiates a restore. This is a DANGEROUS operation.
// If req.TargetTime is set, performs Point-In-Time Recovery (PITR).
func (w *WalG) RestoreBackup(ctx context.Context, req model.RestoreRequest) (*model.BackupTriggerResponse, error) {
	if req.BackupName == "" {
		return nil, fmt.Errorf("backupName is required")
	}

	isPITR := req.TargetTime != ""
	label := req.BackupName
	if isPITR {
		label = fmt.Sprintf("%s (PITR → %s)", req.BackupName, req.TargetTime)
	}

	job := w.jobs.Start("restore", label, "", "restore queued", map[string]string{
		"backupName": req.BackupName,
	})

	go func() {
		w.jobs.Update(job.ID, "stopping PostgreSQL", "")
		log.Printf("[walg] ⚠️ RESTORE STARTED for: %s", label)

		dataDir := w.dataDir

		// Step 1: Stop PostgreSQL
		log.Println("[walg] step 1/5: stopping PostgreSQL...")
		stopCmd := exec.Command("pg_ctl", "stop", "-D", dataDir, "-m", "fast")
		setPgEnv(stopCmd)
		if out, err := stopCmd.CombinedOutput(); err != nil {
			w.jobs.Fail(job.ID, "restore failed while stopping PostgreSQL", string(out))
			log.Printf("[walg] pg_ctl stop failed: %s %v", string(out), err)
			return
		}
		log.Println("[walg] PostgreSQL stopped")

		// Step 2: Clean data directory (keep the directory itself)
		w.jobs.Update(job.ID, "cleaning data directory", "")
		log.Println("[walg] step 2/5: cleaning data directory...")
		cleanCmd := exec.Command("bash", "-c", fmt.Sprintf("rm -rf %s/*", dataDir))
		if out, err := cleanCmd.CombinedOutput(); err != nil {
			w.jobs.Fail(job.ID, "restore failed while cleaning data directory", string(out))
			log.Printf("[walg] clean data dir failed: %s %v", string(out), err)
			return
		}
		log.Println("[walg] data directory cleaned")

		// Step 3: Fetch backup from S3
		w.jobs.Update(job.ID, "fetching backup from storage", "")
		log.Printf("[walg] step 3/5: fetching backup %s from S3...", req.BackupName)
		fetchCmd := exec.Command("wal-g", "backup-fetch", dataDir, req.BackupName)
		setPgEnv(fetchCmd)
		if out, err := fetchCmd.CombinedOutput(); err != nil {
			w.jobs.Fail(job.ID, "restore failed while fetching backup", string(out))
			log.Printf("[walg] backup-fetch failed: %s %v", string(out), err)
			return
		}
		log.Println("[walg] backup fetched successfully")

		// Step 4: Create recovery.signal and set restore_command
		w.jobs.Update(job.ID, "writing recovery configuration", "")
		log.Println("[walg] step 4/5: configuring recovery...")
		signalPath := dataDir + "/recovery.signal"
		if err := os.WriteFile(signalPath, []byte(""), 0644); err != nil {
			w.jobs.Fail(job.ID, "restore failed while writing recovery.signal", err.Error())
			log.Printf("[walg] failed to create recovery.signal: %v", err)
			return
		}

		// Build recovery config lines
		autoConf := dataDir + "/postgresql.auto.conf"
		recoveryLines := "\nrestore_command = 'wal-g wal-fetch \"%f\" \"%p\"'\n"
		if isPITR {
			recoveryLines += fmt.Sprintf("recovery_target_time = '%s'\n", req.TargetTime)
			recoveryLines += "recovery_target_action = 'promote'\n"
		}

		f, err := os.OpenFile(autoConf, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			w.jobs.Fail(job.ID, "restore failed while opening postgresql.auto.conf", err.Error())
			log.Printf("[walg] failed to open postgresql.auto.conf: %v", err)
			return
		}
		if _, err := f.WriteString(recoveryLines); err != nil {
			f.Close()
			w.jobs.Fail(job.ID, "restore failed while writing recovery configuration", err.Error())
			log.Printf("[walg] failed to write recovery config: %v", err)
			return
		}
		f.Close()
		log.Println("[walg] recovery configured")

		// Step 5: Start PostgreSQL
		w.jobs.Update(job.ID, "starting PostgreSQL", "")
		log.Println("[walg] step 5/5: starting PostgreSQL...")
		startCmd := exec.Command("pg_ctl", "start", "-D", dataDir, "-l", "/tmp/pg_restore.log")
		setPgEnv(startCmd)
		if out, err := startCmd.CombinedOutput(); err != nil {
			w.jobs.Fail(job.ID, "restore failed while starting PostgreSQL", string(out))
			log.Printf("[walg] pg_ctl start failed: %s %v", string(out), err)
			return
		}
		w.jobs.Complete(job.ID, "restore completed and PostgreSQL is starting")
		log.Printf("[walg] ✅ RESTORE COMPLETED for: %s — PostgreSQL is starting", label)
	}()

	msg := fmt.Sprintf("Restore started for %s. PostgreSQL will restart.", req.BackupName)
	if isPITR {
		msg = fmt.Sprintf("PITR restore started → %s. PostgreSQL will restart.", req.TargetTime)
	}
	return &model.BackupTriggerResponse{
		Message: msg,
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
