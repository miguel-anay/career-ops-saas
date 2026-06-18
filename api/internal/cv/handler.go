package cv

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
	"github.com/miguel-anay/career-ops-saas/api/internal/middleware"
	"github.com/miguel-anay/career-ops-saas/api/internal/platform"
)

// maxRawCVLength caps the accepted raw CV body size (100KB).
const maxRawCVLength = 100 * 1024

// Servicer is the interface that handlers depend on.
type Servicer interface {
	EnqueuePDFGeneration(ctx context.Context, userID, jobID uuid.UUID) (string, error)
	GetDownloadURL(ctx context.Context, r2 *platform.R2Client, userID, jobID uuid.UUID) (string, time.Time, error)
	ListCVs(ctx context.Context, userID uuid.UUID) ([]db.Cv, error)
	CreateCV(ctx context.Context, userID uuid.UUID, title, contentMd string, isMaster bool) (*db.Cv, error)
	EnqueueIngest(ctx context.Context, userID uuid.UUID, rawCV string) (uuid.UUID, error)
	GetIngestion(ctx context.Context, userID, runID uuid.UUID) (*db.CvIngestion, error)
}

// Handler holds dependencies for cv HTTP handlers.
type Handler struct {
	svc Servicer
	r2  *platform.R2Client
}

// NewHandler creates a new cv Handler.
func NewHandler(svc Servicer, r2 *platform.R2Client) *Handler {
	return &Handler{svc: svc, r2: r2}
}

// RegisterRoutes mounts the cv routes onto a chi.Router.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/jobs/{id}/cv", h.EnqueueCV)
	r.Get("/api/jobs/{id}/cv", h.GetCV)
	r.Get("/api/cvs", h.ListCVs)
	r.Post("/api/cvs", h.CreateCV)
	r.Post("/api/cv/ingest", h.Ingest)
	r.Get("/api/cv/ingest/{id}", h.GetIngestion)
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

// Ingest handles POST /api/cv/ingest {raw_cv}
func (h *Handler) Ingest(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing_user_id")
		return
	}

	var body struct {
		RawCV string `json:"raw_cv"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "invalid_body")
		return
	}

	if strings.TrimSpace(body.RawCV) == "" {
		writeError(w, http.StatusBadRequest, "raw_cv is required", "invalid_body")
		return
	}
	if len(body.RawCV) > maxRawCVLength {
		writeError(w, http.StatusBadRequest, "raw_cv exceeds maximum length", "invalid_body")
		return
	}

	runID, err := h.svc.EnqueueIngest(r.Context(), userID, body.RawCV)
	if err != nil {
		switch {
		case errors.Is(err, ErrUsageLimitExceeded):
			writeError(w, http.StatusPaymentRequired, err.Error(), "usage_limit_exceeded")
		default:
			writeError(w, http.StatusInternalServerError, "failed to enqueue ingestion", "internal_error")
		}
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"run_id": runID,
	})
}

// GetIngestion handles GET /api/cv/ingest/{id}
func (h *Handler) GetIngestion(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing_user_id")
		return
	}

	idStr := chi.URLParam(r, "id")
	runID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid ingestion id", "invalid_id")
		return
	}

	ingestion, err := h.svc.GetIngestion(r.Context(), userID, runID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "ingestion not found", "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get ingestion", "internal_error")
		return
	}

	var finishedAt interface{}
	if ingestion.FinishedAt.Valid {
		finishedAt = ingestion.FinishedAt.Time
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":          ingestion.ID,
		"status":      ingestion.Status,
		"started_at":  ingestion.StartedAt,
		"finished_at": finishedAt,
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg, code string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}
