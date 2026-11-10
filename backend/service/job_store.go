package service

import (
	"fmt"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type JobStatus string

const (
	JobStatusRunning   JobStatus = "running"
	JobStatusSucceeded JobStatus = "succeeded"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCanceled  JobStatus = "canceled"
)

type Job struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Target      string            `json:"target"`
	Database    string            `json:"database,omitempty"`
	Status      JobStatus         `json:"status"`
	Message     string            `json:"message,omitempty"`
	Error       string            `json:"error,omitempty"`
	Details     string            `json:"details,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	CreatedAt   time.Time         `json:"createdAt"`
	StartedAt   time.Time         `json:"startedAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
	FinishedAt  *time.Time        `json:"finishedAt,omitempty"`
	Artifact    *JobArtifact      `json:"artifact,omitempty"`
}

type JobArtifact struct {
	Path        string `json:"-"`
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	SizeBytes   int64  `json:"sizeBytes"`
}

type JobStore struct {
	mu        sync.RWMutex
	jobs      map[string]*Job
	order     []string
	maxJobs   int
	cleanupFn func(*Job)
	seq       atomic.Uint64
}

func NewJobStore() *JobStore {
	return &JobStore{
		jobs:    make(map[string]*Job),
		maxJobs: 200,
		cleanupFn: func(job *Job) {
			if job.Artifact != nil && job.Artifact.Path != "" {
				_ = os.Remove(job.Artifact.Path)
			}
		},
	}
}

func (s *JobStore) Start(jobType, target, database, message string, metadata map[string]string) *Job {
	now := time.Now()
	job := &Job{
		ID:        fmt.Sprintf("job-%d-%06d", now.Unix(), s.seq.Add(1)),
		Type:      jobType,
		Target:    target,
		Database:  database,
		Status:    JobStatusRunning,
		Message:   message,
		Metadata:  cloneMetadata(metadata),
		CreatedAt: now,
		StartedAt: now,
		UpdatedAt: now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.ID] = job
	s.order = append([]string{job.ID}, s.order...)
	s.trimLocked()
	return cloneJob(job)
}

func (s *JobStore) Update(jobID, message, details string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return
	}
	if message != "" {
		job.Message = message
	}
	if details != "" {
		job.Details = details
	}
	job.UpdatedAt = time.Now()
}

func (s *JobStore) Complete(jobID, message string) {
	s.finish(jobID, JobStatusSucceeded, message, "", nil)
}

func (s *JobStore) CompleteWithArtifact(jobID, message string, artifact *JobArtifact) {
	s.finish(jobID, JobStatusSucceeded, message, "", artifact)
}

func (s *JobStore) Fail(jobID, message, details string) {
	s.finish(jobID, JobStatusFailed, message, details, nil)
}

func (s *JobStore) Cancel(jobID, message string) {
	s.finish(jobID, JobStatusCanceled, message, "", nil)
}

func (s *JobStore) finish(jobID string, status JobStatus, message, details string, artifact *JobArtifact) {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return
	}
	now := time.Now()
	job.Status = status
	job.Message = message
	if status == JobStatusFailed {
		job.Error = message
	}
	if details != "" {
		job.Details = details
	}
	if artifact != nil {
		job.Artifact = artifact
	}
	job.UpdatedAt = now
	job.FinishedAt = &now
}

func (s *JobStore) Get(jobID string) *Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return nil
	}
	return cloneJob(job)
}

func (s *JobStore) List(jobType string, limit int) []Job {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 || limit > s.maxJobs {
		limit = 50
	}

	result := make([]Job, 0, limit)
	for _, id := range s.order {
		job, ok := s.jobs[id]
		if !ok {
			continue
		}
		if jobType != "" && job.Type != jobType {
			continue
		}
		result = append(result, *cloneJob(job))
		if len(result) >= limit {
			break
		}
	}

	sort.SliceStable(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result
}

func (s *JobStore) trimLocked() {
	for len(s.order) > s.maxJobs {
		last := s.order[len(s.order)-1]
		s.order = s.order[:len(s.order)-1]
		if job, ok := s.jobs[last]; ok {
			s.cleanupFn(job)
			delete(s.jobs, last)
		}
	}
}

func cloneJob(job *Job) *Job {
	if job == nil {
		return nil
	}
	cp := *job
	cp.Metadata = cloneMetadata(job.Metadata)
	if job.Artifact != nil {
		artifact := *job.Artifact
		cp.Artifact = &artifact
	}
	if job.FinishedAt != nil {
		finished := *job.FinishedAt
		cp.FinishedAt = &finished
	}
	return &cp
}

func cloneMetadata(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
