package scan

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
	TriggerScan(ctx context.Context, userID uuid.UUID) (uuid.UUID, error)
	GetScanRun(ctx context.Context, userID, scanRunID uuid.UUID) (*db.ScanRun, error)
}

// Handler holds dependencies for scan HTTP handlers.
type Handler struct {
	svc Servicer
}

// NewHandler creates a new scan Handler.
func NewHandler(svc Servicer) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes mounts the scan routes onto a chi.Router.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/scan", h.TriggerScan)
	r.Get("/api/scan-runs/{id}", h.GetScanRun)
}

// TriggerScan handles POST /api/scan
func (h *Handler) TriggerScan(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing_user_id")
		return
	}

	scanRunID, err := h.svc.TriggerScan(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to trigger scan", "internal_error")
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"scan_run_id": scanRunID,
	})
}

// GetScanRun handles GET /api/scan-runs/{id}
func (h *Handler) GetScanRun(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing_user_id")
		return
	}

	idStr := chi.URLParam(r, "id")
	scanRunID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid scan run id", "invalid_id")
		return
	}

	scanRun, err := h.svc.GetScanRun(r.Context(), userID, scanRunID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "scan run not found", "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get scan run", "internal_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":          scanRun.ID,
		"status":      scanRun.Status,
		"new_jobs":    scanRun.NewJobs,
		"errors":      scanRun.ErrorsJson,
		"started_at":  scanRun.StartedAt,
		"finished_at": scanRun.FinishedAt,
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
