package tracker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
	"github.com/miguel-anay/career-ops-saas/api/internal/middleware"
)

// Servicer is the interface that handlers depend on.
type Servicer interface {
	ListApplications(ctx context.Context, userID uuid.UUID, page, limit int) ([]db.Application, error)
	UpdateApplication(ctx context.Context, userID, appID uuid.UUID, status *string, notes *string) (*db.Application, error)
}

// Handler holds dependencies for tracker HTTP handlers.
type Handler struct {
	svc Servicer
}

// NewHandler creates a new tracker Handler.
func NewHandler(svc Servicer) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes mounts the tracker routes onto a chi.Router.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/applications", h.List)
	r.Patch("/api/applications/{id}", h.Update)
}

// List handles GET /api/applications?page=1&limit=20
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing_user_id")
		return
	}

	page := queryIntDefault(r, "page", 1)
	limit := queryIntDefault(r, "limit", 20)
	if limit > 100 {
		limit = 100
	}

	apps, err := h.svc.ListApplications(r.Context(), userID, page, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list applications", "internal_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"applications": apps,
		"page":         page,
		"limit":        limit,
	})
}

// Update handles PATCH /api/applications/{id} {status?, notes?}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing_user_id")
		return
	}

	idStr := chi.URLParam(r, "id")
	appID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid application id", "invalid_id")
		return
	}

	var body struct {
		Status *string `json:"status"`
		Notes  *string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "invalid_request")
		return
	}

	if body.Status == nil && body.Notes == nil {
		writeError(w, http.StatusBadRequest, "at least one of status or notes must be provided", "invalid_request")
		return
	}

	app, err := h.svc.UpdateApplication(r.Context(), userID, appID, body.Status, body.Notes)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusNotFound, "application not found", "not_found")
		case errors.Is(err, ErrInvalidStatus):
			writeError(w, http.StatusBadRequest, err.Error(), "invalid_status")
		default:
			writeError(w, http.StatusInternalServerError, "failed to update application", "internal_error")
		}
		return
	}

	writeJSON(w, http.StatusOK, app)
}

func queryIntDefault(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return def
	}
	return n
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg, code string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}
