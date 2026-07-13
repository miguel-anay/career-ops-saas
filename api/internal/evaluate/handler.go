package evaluate

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
	"github.com/miguel-anay/career-ops-saas/api/internal/middleware"
)

// Servicer is the interface that handlers depend on.
type Servicer interface {
	EnqueueEvaluation(ctx context.Context, userID, jobID uuid.UUID) (string, error)
	GetReport(ctx context.Context, userID, jobID uuid.UUID) (*db.Report, error)
}

// Handler holds dependencies for evaluate HTTP handlers.
type Handler struct {
	svc Servicer
}

// NewHandler creates a new evaluate Handler.
func NewHandler(svc Servicer) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes mounts the evaluate routes onto a chi.Router.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/jobs/{id}/evaluate", h.Evaluate)
	r.Get("/api/jobs/{id}/report", h.GetReport)
}

// Evaluate handles POST /api/jobs/{id}/evaluate
func (h *Handler) Evaluate(w http.ResponseWriter, r *http.Request) {
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

	queueID, err := h.svc.EnqueueEvaluation(r.Context(), userID, jobID)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusNotFound, "job not found", "not_found")
		case errors.Is(err, ErrUsageLimitExceeded):
			writeError(w, http.StatusPaymentRequired, err.Error(), "usage_limit_exceeded")
		case errors.Is(err, ErrCVMissing):
			writeError(w, http.StatusUnprocessableEntity, err.Error(), "cv_missing")
		case errors.Is(err, ErrJobContentMissing):
			writeError(w, http.StatusUnprocessableEntity, err.Error(), "job_content_missing")
		case errors.Is(err, ErrStalePosting):
			writeError(w, http.StatusUnprocessableEntity, err.Error(), "stale_posting")
		default:
			writeError(w, http.StatusInternalServerError, "failed to enqueue evaluation", "internal_error")
		}
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"job_id":   jobID,
		"queued":   true,
		"queue_id": queueID,
	})
}

// GetReport handles GET /api/jobs/{id}/report
func (h *Handler) GetReport(w http.ResponseWriter, r *http.Request) {
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

	report, err := h.svc.GetReport(r.Context(), userID, jobID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "report not found", "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get report", "internal_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"blocks_json": report.BlocksJson,
		"content_md":  report.ContentMd,
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
