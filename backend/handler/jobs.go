package handler

import (
	"net/http"

	"pgaio/model"
	"pgaio/service"
)

type JobsHandler struct {
	jobs *service.JobStore
}

func NewJobsHandler(jobs *service.JobStore) *JobsHandler {
	return &JobsHandler{jobs: jobs}
}

func (h *JobsHandler) ListJobs(w http.ResponseWriter, r *http.Request) {
	jobType := r.URL.Query().Get("type")
	jobs := h.jobs.List(jobType, 100)
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: jobs})
}

func (h *JobsHandler) GetJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	job := h.jobs.Get(jobID)
	if job == nil {
		writeJSON(w, http.StatusNotFound, model.APIResponse{Error: "job not found"})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: job})
}

func (h *JobsHandler) DownloadArtifact(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	job := h.jobs.Get(jobID)
	if job == nil {
		writeJSON(w, http.StatusNotFound, model.APIResponse{Error: "job not found"})
		return
	}
	if job.Artifact == nil || job.Artifact.Path == "" {
		writeJSON(w, http.StatusNotFound, model.APIResponse{Error: "job artifact not available"})
		return
	}

	w.Header().Set("Content-Type", job.Artifact.ContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+job.Artifact.Name+`"`)
	http.ServeFile(w, r, job.Artifact.Path)
}
