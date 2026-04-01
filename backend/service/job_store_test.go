package service

import "testing"

func TestJobStoreLifecycle(t *testing.T) {
	store := NewJobStore()

	job := store.Start("backup", "cluster", "postgres", "queued", map[string]string{"kind": "full"})
	if job.ID == "" {
		t.Fatal("expected job ID")
	}
	if job.Status != JobStatusRunning {
		t.Fatalf("expected running status, got %s", job.Status)
	}

	store.Update(job.ID, "in progress", "step 1")
	store.CompleteWithArtifact(job.ID, "done", &JobArtifact{
		Path:        "/tmp/pgaio-test",
		Name:        "backup.dump",
		ContentType: "application/octet-stream",
		SizeBytes:   42,
	})

	got := store.Get(job.ID)
	if got == nil {
		t.Fatal("expected stored job")
	}
	if got.Status != JobStatusSucceeded {
		t.Fatalf("expected succeeded status, got %s", got.Status)
	}
	if got.Artifact == nil || got.Artifact.Name != "backup.dump" {
		t.Fatalf("expected artifact metadata, got %#v", got.Artifact)
	}
	if got.Metadata["kind"] != "full" {
		t.Fatalf("expected metadata to be preserved, got %#v", got.Metadata)
	}

	list := store.List("", 10)
	if len(list) != 1 {
		t.Fatalf("expected one job, got %d", len(list))
	}
}
