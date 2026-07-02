package emailingest

import (
	"context"
	"database/sql"
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
	TriggerIngest(ctx context.Context, userID uuid.UUID) (uuid.UUID, error)
	GetIngestRun(ctx context.Context, userID, ingestRunID uuid.UUID) (*db.EmailIngestRun, error)
}

// Handler holds dependencies for emailingest HTTP handlers.
type Handler struct {
	svc Servicer
}

// NewHandler creates a new emailingest Handler.
func NewHandler(svc Servicer) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes mounts the emailingest routes onto a chi.Router.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/email-ingest", h.TriggerIngest)
	r.Get("/api/email-ingest-runs/{id}", h.GetIngestRun)
}

// TriggerIngest handles POST /api/email-ingest
func (h *Handler) TriggerIngest(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing_user_id")
		return
	}

	runID, err := h.svc.TriggerIngest(r.Context(), userID)
	if err != nil {
		if errors.Is(err, ErrGmailNotConnected) {
			writeError(w, http.StatusUnprocessableEntity, "gmail_not_connected", "gmail_not_connected")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to trigger email ingest", "internal_error")
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"ingest_run_id": runID,
	})
}

// GetIngestRun handles GET /api/email-ingest-runs/{id}
func (h *Handler) GetIngestRun(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing_user_id")
		return
	}

	idStr := chi.URLParam(r, "id")
	ingestRunID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid ingest run id", "invalid_id")
		return
	}

	run, err := h.svc.GetIngestRun(r.Context(), userID, ingestRunID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "email ingest run not found", "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get email ingest run", "internal_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":          run.ID,
		"status":      run.Status,
		"new_jobs":    run.NewJobs,
		"errors":      run.ErrorsJson,
		"started_at":  run.StartedAt,
		"finished_at": run.FinishedAt,
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
