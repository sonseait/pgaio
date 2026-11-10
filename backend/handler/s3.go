package handler

import (
	"net/http"

	"pgaio/model"
	"pgaio/service"
)

type S3Handler struct {
	s3 *service.S3Client
}

func NewS3Handler(s3 *service.S3Client) *S3Handler {
	return &S3Handler{s3: s3}
}

// ListObjects lists S3 objects with optional prefix.
func (h *S3Handler) ListObjects(w http.ResponseWriter, r *http.Request) {
	if h.s3 == nil {
		writeJSON(w, http.StatusServiceUnavailable, model.APIResponse{Error: "S3 not configured"})
		return
	}
	prefix := r.URL.Query().Get("prefix")
	delimiter := r.URL.Query().Get("delimiter")
	if delimiter == "" {
		delimiter = "/"
	}

	list, err := h.s3.ListObjects(r.Context(), prefix, delimiter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: list})
}

// DeleteObject deletes an S3 object.
func (h *S3Handler) DeleteObject(w http.ResponseWriter, r *http.Request) {
	if h.s3 == nil {
		writeJSON(w, http.StatusServiceUnavailable, model.APIResponse{Error: "S3 not configured"})
		return
	}
	key := r.URL.Query().Get("key")
	if key == "" {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "key parameter required"})
		return
	}

	if err := h.s3.DeleteObject(r.Context(), key); err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: "deleted"})
}

// GetDownloadURL returns a pre-signed download URL.
func (h *S3Handler) GetDownloadURL(w http.ResponseWriter, r *http.Request) {
	if h.s3 == nil {
		writeJSON(w, http.StatusServiceUnavailable, model.APIResponse{Error: "S3 not configured"})
		return
	}
	key := r.URL.Query().Get("key")
	if key == "" {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "key parameter required"})
		return
	}

	url, err := h.s3.GetPresignedURL(r.Context(), key)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: map[string]string{"url": url}})
}
