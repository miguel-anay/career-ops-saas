package cv

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/middleware"
	"github.com/miguel-anay/career-ops-saas/api/internal/platform"
)

// Handler holds dependencies for cv HTTP handlers.
type Handler struct {
	svc *Service
	r2  *platform.R2Client
}

// NewHandler creates a new cv Handler.
func NewHandler(svc *Service, r2 *platform.R2Client) *Handler {
	return &Handler{svc: svc, r2: r2}
}

// RegisterRoutes mounts the cv routes onto a chi.Router.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/jobs/{id}/cv", h.EnqueueCV)
	r.Get("/api/jobs/{id}/cv", h.GetCV)
	r.Get("/api/cvs", h.ListCVs)
	r.Post("/api/cvs", h.CreateCV)
}

// EnqueueCV handles POST /api/jobs/{id}/cv
func (h *Handler) EnqueueCV(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing_user_id")
		return
	}

	idStr := chi.URLParam(r, "id")
	jobID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id", "invalid_id")
		return
	}

	queueID, err := h.svc.EnqueuePDFGeneration(r.Context(), userID, jobID)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusNotFound, "job or application not found", "not_found")
		default:
			writeError(w, http.StatusBadRequest, err.Error(), "invalid_request")
		}
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"job_id":   jobID,
		"queued":   true,
		"queue_id": queueID,
	})
}

// GetCV handles GET /api/jobs/{id}/cv
func (h *Handler) GetCV(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing_user_id")
		return
	}

	idStr := chi.URLParam(r, "id")
	jobID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id", "invalid_id")
		return
	}

	if h.r2 == nil {
		writeError(w, http.StatusServiceUnavailable, "R2 storage not configured", "r2_unavailable")
		return
	}

	downloadURL, expiresAt, err := h.svc.GetDownloadURL(r.Context(), h.r2, userID, jobID)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusNotFound, "CV not found", "not_found")
		case errors.Is(err, ErrNoPDFPath):
			writeError(w, http.StatusNotFound, "PDF not yet generated", "pdf_not_ready")
		default:
			writeError(w, http.StatusInternalServerError, "failed to get download URL", "internal_error")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"download_url": downloadURL,
		"expires_at":   expiresAt,
	})
}

// ListCVs handles GET /api/cvs
func (h *Handler) ListCVs(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing_user_id")
		return
	}

	cvs, err := h.svc.ListCVs(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list CVs", "internal_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"cvs": cvs,
	})
}

// CreateCV handles POST /api/cvs {title, content_md, is_master}
func (h *Handler) CreateCV(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing_user_id")
		return
	}

	var body struct {
		Title     string `json:"title"`
		ContentMd string `json:"content_md"`
		IsMaster  bool   `json:"is_master"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "invalid_request")
		return
	}

	cvRecord, err := h.svc.CreateCV(r.Context(), userID, body.Title, body.ContentMd, body.IsMaster)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request")
		return
	}

	writeJSON(w, http.StatusCreated, cvRecord)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg, code string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}
