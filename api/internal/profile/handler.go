package profile

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/middleware"
)

// Handler holds dependencies for profile HTTP handlers.
type Handler struct {
	svc Servicer
}

// NewHandler creates a new profile Handler.
func NewHandler(svc Servicer) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes mounts the profile routes onto a chi.Router.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/me/profile", h.GetProfile)
	r.Patch("/api/me/profile", h.PatchProfile)
	r.Post("/api/me/profile-edits/{id}/undo", h.UndoEdit)
}

// GetProfile handles GET /api/me/profile
func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing_user_id")
		return
	}

	result, err := h.svc.GetProfile(r.Context(), userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "profile not found", "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get profile", "internal_error")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// PatchProfile handles PATCH /api/me/profile {field_path, value}
func (h *Handler) PatchProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing_user_id")
		return
	}

	var body struct {
		FieldPath string          `json:"field_path"`
		Value     json.RawMessage `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "invalid_body")
		return
	}

	edit, err := h.svc.ApplyOverride(r.Context(), userID, body.FieldPath, body.Value)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidFieldPath):
			writeError(w, http.StatusBadRequest, err.Error(), "invalid_field_path")
		default:
			writeError(w, http.StatusInternalServerError, "failed to apply override", "internal_error")
		}
		return
	}

	writeJSON(w, http.StatusOK, edit)
}

// UndoEdit handles POST /api/me/profile-edits/{id}/undo
func (h *Handler) UndoEdit(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing_user_id")
		return
	}

	idStr := chi.URLParam(r, "id")
	editID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid edit id", "invalid_id")
		return
	}

	if err := h.svc.UndoEdit(r.Context(), userID, editID); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "edit not found", "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to undo edit", "internal_error")
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
