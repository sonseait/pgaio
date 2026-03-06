package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"pgaio/model"
)

// WalG wraps wal-g CLI operations.
type WalG struct {
	dataDir string
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

func NewWalG(dataDir string) *WalG {
	if dataDir == "" {
		dataDir = "/bitnami/postgresql/data"
	}
	return &WalG{dataDir: dataDir}
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
	go func() {
		log.Println("[walg] starting manual backup...")
		cmd := exec.Command("wal-g", "backup-push", w.dataDir)
		setPgEnv(cmd)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			log.Printf("[walg] manual backup failed: %s: %v", stderr.String(), err)
			return
		}
		log.Println("[walg] manual backup completed successfully")
	}()

	return &model.BackupTriggerResponse{
		Message: "Backup triggered in background",
		Status:  "running",
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

	go func() {
		log.Printf("[walg] ⚠️ RESTORE STARTED for: %s", label)

		dataDir := w.dataDir

		// Step 1: Stop PostgreSQL
		log.Println("[walg] step 1/5: stopping PostgreSQL...")
		stopCmd := exec.Command("pg_ctl", "stop", "-D", dataDir, "-m", "fast")
		setPgEnv(stopCmd)
		if out, err := stopCmd.CombinedOutput(); err != nil {
			log.Printf("[walg] pg_ctl stop failed: %s %v", string(out), err)
			return
		}
		log.Println("[walg] PostgreSQL stopped")

		// Step 2: Clean data directory (keep the directory itself)
		log.Println("[walg] step 2/5: cleaning data directory...")
		cleanCmd := exec.Command("bash", "-c", fmt.Sprintf("rm -rf %s/*", dataDir))
		if out, err := cleanCmd.CombinedOutput(); err != nil {
			log.Printf("[walg] clean data dir failed: %s %v", string(out), err)
			return
		}
		log.Println("[walg] data directory cleaned")

		// Step 3: Fetch backup from S3
		log.Printf("[walg] step 3/5: fetching backup %s from S3...", req.BackupName)
		fetchCmd := exec.Command("wal-g", "backup-fetch", dataDir, req.BackupName)
		setPgEnv(fetchCmd)
		if out, err := fetchCmd.CombinedOutput(); err != nil {
			log.Printf("[walg] backup-fetch failed: %s %v", string(out), err)
			return
		}
		log.Println("[walg] backup fetched successfully")

		// Step 4: Create recovery.signal and set restore_command
		log.Println("[walg] step 4/5: configuring recovery...")
		signalPath := dataDir + "/recovery.signal"
		if err := os.WriteFile(signalPath, []byte(""), 0644); err != nil {
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
			log.Printf("[walg] failed to open postgresql.auto.conf: %v", err)
			return
		}
		if _, err := f.WriteString(recoveryLines); err != nil {
			f.Close()
			log.Printf("[walg] failed to write recovery config: %v", err)
			return
		}
		f.Close()
		log.Println("[walg] recovery configured")

		// Step 5: Start PostgreSQL
		log.Println("[walg] step 5/5: starting PostgreSQL...")
		startCmd := exec.Command("pg_ctl", "start", "-D", dataDir, "-l", "/tmp/pg_restore.log")
		setPgEnv(startCmd)
		if out, err := startCmd.CombinedOutput(); err != nil {
			log.Printf("[walg] pg_ctl start failed: %s %v", string(out), err)
			return
		}
		log.Printf("[walg] ✅ RESTORE COMPLETED for: %s — PostgreSQL is starting", label)
	}()

	msg := fmt.Sprintf("Restore started for %s. PostgreSQL will restart.", req.BackupName)
	if isPITR {
		msg = fmt.Sprintf("PITR restore started → %s. PostgreSQL will restart.", req.TargetTime)
	}
	return &model.BackupTriggerResponse{
		Message: msg,
		Status:  "running",
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
