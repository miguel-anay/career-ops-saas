package digest

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

// Servicer is the interface Handler depends on.
type Servicer interface {
	ListDigests(ctx context.Context, userID uuid.UUID) ([]db.ArticleDigest, error)
	CreateDigest(ctx context.Context, userID uuid.UUID, title, contentMd string) (*db.ArticleDigest, error)
	DeleteDigest(ctx context.Context, userID, digestID uuid.UUID) error
}

// Handler holds dependencies for digest HTTP handlers.
type Handler struct {
	svc Servicer
}

// NewHandler creates a new digest Handler.
func NewHandler(svc Servicer) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes mounts the article-digest routes onto a chi.Router.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/article-digests", h.List)
	r.Post("/api/article-digests", h.Create)
	r.Delete("/api/article-digests/{id}", h.Delete)
}

// List handles GET /api/article-digests
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing_user_id")
		return
	}

	digests, err := h.svc.ListDigests(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list digests", "internal_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"digests": digests,
	})
}

// Create handles POST /api/article-digests {title, content_md}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing_user_id")
		return
	}

	var body struct {
		Title     string `json:"title"`
		ContentMd string `json:"content_md"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "invalid_request")
		return
	}

	digestRecord, err := h.svc.CreateDigest(r.Context(), userID, body.Title, body.ContentMd)
	if err != nil {
		if errors.Is(err, ErrValidation) {
			writeError(w, http.StatusBadRequest, err.Error(), "invalid_request")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create digest", "internal_error")
		return
	}

	writeJSON(w, http.StatusCreated, digestRecord)
}

// Delete handles DELETE /api/article-digests/{id}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing_user_id")
		return
	}

	idStr := chi.URLParam(r, "id")
	digestID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid digest id", "invalid_id")
		return
	}

	if err := h.svc.DeleteDigest(r.Context(), userID, digestID); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "digest not found", "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete digest", "internal_error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg, code string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}
