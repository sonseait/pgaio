package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pgaio/model"
)

func TestSelectRestoreBackup(t *testing.T) {
	start := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	backups := []model.Backup{
		{Name: "newest", StartTime: start.Add(4 * time.Hour), FinishTime: start.Add(5 * time.Hour)},
		{Name: "full", StartTime: start, FinishTime: start.Add(time.Hour)},
		{Name: "delta", StartTime: start.Add(2 * time.Hour), FinishTime: start.Add(3 * time.Hour)},
		{Name: "unknown-finish", StartTime: start},
	}
	for _, tt := range []struct {
		name   string
		target time.Time
		want   string
	}{
		{"LATEST", time.Time{}, "newest"},
		{"latest", start.Add(3 * time.Hour), "delta"},
		{"LATEST", start.Add(150 * time.Minute), "full"}, // target during delta backup
		{"full", start.Add(3 * time.Hour), "full"},
		{"newest", start.Add(3 * time.Hour), ""},
		{"LATEST", start, ""},
		{"missing", time.Time{}, ""},
		{"unknown-finish", start.Add(time.Hour), ""},
	} {
		got, err := selectRestoreBackup(backups, tt.name, tt.target)
		if got != tt.want || (err != nil) != (tt.want == "") {
			t.Errorf("select(%s, %s) = %q, %v; want %q", tt.name, tt.target, got, err, tt.want)
		}
	}
}

func TestConfigureRecoveryNormalizesTimeAndReplacesOldTargets(t *testing.T) {
	for _, input := range []string{"2026-11-06T22:06:58.999321Z", "2026-11-07T02:06:58.999321+04:00", "2026-11-06 22:06:58.999321"} {
		t.Run(input, func(t *testing.T) {
			dir := t.TempDir()
			conf := filepath.Join(dir, "postgresql.auto.conf")
			old := "# preserved comment\nshared_buffers = '128MB'\nrecovery_target = 'immediate'\nrecovery_target_lsn = '0/123'\nrecovery_target_time = 'old'\nrestore_command = 'old'\n"
			if err := os.WriteFile(conf, []byte(old), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "standby.signal"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			target, err := parseWalgTime(input)
			if err != nil {
				t.Fatal(err)
			}
			if err := configureRecovery(dir, target); err != nil {
				t.Fatal(err)
			}
			content, _ := os.ReadFile(conf)
			text := string(content)
			if !strings.Contains(text, "recovery_target_time = '2026-11-06 22:06:58.999321+00:00'") || strings.Contains(text, "'old'") || strings.Contains(text, "'0/123'") || strings.Contains(text, "'immediate'") {
				t.Fatalf("incorrect recovery config:\n%s", text)
			}
			if !strings.Contains(text, "shared_buffers = '128MB'") {
				t.Fatal("lost non-recovery settings")
			}
			if _, err := os.Stat(filepath.Join(dir, "standby.signal")); !os.IsNotExist(err) {
				t.Fatal("standby.signal was not removed")
			}
			if _, err := os.Stat(filepath.Join(dir, "recovery.signal")); err != nil {
				t.Fatal(err)
			}
			// A subsequent snapshot restore must not inherit this PITR target.
			if err := configureRecovery(dir, time.Time{}); err != nil {
				t.Fatal(err)
			}
			content, _ = os.ReadFile(conf)
			if strings.Contains(string(content), "2026-11-06") || !strings.Contains(string(content), "recovery_target = 'immediate'") {
				t.Fatalf("stale PITR target in snapshot restore:\n%s", content)
			}
		})
	}
}

func TestRestoreFetchFailureDoesNotStopOrDeleteLiveCluster(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.Mkdir(dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	liveFile := filepath.Join(dataDir, "live-data")
	if err := os.WriteFile(liveFile, []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
if [ "$1" = backup-list ]; then
  echo '[{"backup_name":"base","time":"2026-11-06T12:00:00Z","finish_time":"2026-11-06T12:01:00Z"}]'
else
  echo 'storage unavailable' >&2
  exit 1
fi
`
	if err := os.WriteFile(filepath.Join(root, "wal-g"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	stopMarker := filepath.Join(root, "stopped")
	t.Setenv("STOP_MARKER", stopMarker)
	if err := os.WriteFile(filepath.Join(root, "pg_ctl"), []byte("#!/bin/sh\ntouch \"$STOP_MARKER\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	jobs := NewJobStore()
	w := NewWalG(dataDir, jobs, nil)
	if _, err := w.RestoreBackup(context.Background(), model.RestoreRequest{BackupName: "missing"}); err == nil {
		t.Fatal("accepted an unknown backup")
	}
	resp, err := w.RestoreBackup(context.Background(), model.RestoreRequest{BackupName: "base"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for jobs.Get(resp.JobID).Status == JobStatusRunning && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	job := jobs.Get(resp.JobID)
	if job.Status != JobStatusFailed || !strings.Contains(job.Details, "storage unavailable") {
		t.Fatalf("unexpected job: %+v", job)
	}
	if contents, err := os.ReadFile(liveFile); err != nil || string(contents) != "keep me" {
		t.Fatalf("live data changed: %q, %v", contents, err)
	}
	if _, err := os.Stat(stopMarker); !os.IsNotExist(err) {
		t.Fatal("PostgreSQL was stopped before backup fetch succeeded")
	}
	staging, _ := filepath.Glob(filepath.Join(dataDir, ".pgaio-restore-*"))
	if len(staging) != 0 {
		t.Fatalf("staging directories leaked: %v", staging)
	}
}

func TestInstallRestorePreservesStagingOnDataVolume(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old-data")
	if err := os.WriteFile(old, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := installRestore(dir, t.TempDir()); err == nil {
		t.Fatal("accepted staging outside data volume")
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("invalid staging removed live data")
	}
	stage, err := os.MkdirTemp(dir, ".pgaio-restore-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "new-data"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := installRestore(dir, stage); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "new-data")); err != nil || string(data) != "new" {
		t.Fatalf("missing restored data: %q, %v", data, err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old data still present")
	}
	if entries, err := os.ReadDir(stage); err != nil || len(entries) != 0 {
		t.Fatalf("staging was removed or not emptied: %v, %v", entries, err)
	}
}
