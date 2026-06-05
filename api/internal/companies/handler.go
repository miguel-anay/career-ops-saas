package companies

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
	List(ctx context.Context, userID uuid.UUID) ([]db.WatchedCompany, error)
	Add(ctx context.Context, userID uuid.UUID, name, careersURL, providerID string) (*db.WatchedCompany, error)
	Remove(ctx context.Context, userID uuid.UUID, companyID uuid.UUID) error
}

// Handler holds dependencies for companies HTTP handlers.
type Handler struct {
	svc Servicer
}

// NewHandler creates a new companies Handler.
func NewHandler(svc Servicer) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes mounts the companies routes onto a chi.Router.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/companies", h.List)
	r.Post("/api/companies", h.Create)
	r.Delete("/api/companies/{id}", h.Delete)
}

// List handles GET /api/companies
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing_user_id")
		return
	}

	companies, err := h.svc.List(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list companies", "internal_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"companies": companies,
	})
}

// Create handles POST /api/companies {name, careers_url, provider_id?}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing_user_id")
		return
	}

	var body struct {
		Name       string `json:"name"`
		CareersURL string `json:"careers_url"`
		ProviderID string `json:"provider_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required", "invalid_request")
		return
	}

	company, err := h.svc.Add(r.Context(), userID, body.Name, body.CareersURL, body.ProviderID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request")
		return
	}

	writeJSON(w, http.StatusCreated, company)
}

// Delete handles DELETE /api/companies/{id}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing_user_id")
		return
	}

	idStr := chi.URLParam(r, "id")
	companyID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid company id", "invalid_id")
		return
	}

	if err := h.svc.Remove(r.Context(), userID, companyID); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "company not found", "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete company", "internal_error")
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
