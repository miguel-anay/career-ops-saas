package companies

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
	"github.com/miguel-anay/career-ops-saas/api/internal/middleware"
)

// companyResponse is the HTTP-facing shape of a watched company. It flattens
// sqlc's sql.NullString columns into plain strings so the persistence layer's
// {String, Valid} struct never leaks across the JSON boundary (an invalid
// NullString serializes to "", which the client renders as empty/absent).
type companyResponse struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	CareersURL string    `json:"careers_url"`
	ProviderID string    `json:"provider_id"`
	AtsAPIURL  string    `json:"ats_api_url"`
	Enabled    bool      `json:"enabled"`
	CompanyID  string    `json:"company_id"`
	CreatedAt  time.Time `json:"created_at"`
}

func toCompanyResponse(c db.WatchedCompany) companyResponse {
	companyID := ""
	if c.CompanyID.Valid {
		companyID = c.CompanyID.UUID.String()
	}
	return companyResponse{
		ID:         c.ID,
		Name:       c.Name,
		CareersURL: c.CareersUrl.String,
		ProviderID: c.ProviderID.String,
		AtsAPIURL:  c.AtsApiUrl.String,
		Enabled:    c.Enabled,
		CompanyID:  companyID,
		CreatedAt:  c.CreatedAt,
	}
}

// catalogResponse is the HTTP-facing shape of a global catalog entry, with the
// nullable ats_api_url flattened to a plain string (see companyResponse).
type catalogResponse struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	CareersURL string    `json:"careers_url"`
	ProviderID string    `json:"provider_id"`
	AtsAPIURL  string    `json:"ats_api_url"`
}

func toCatalogResponse(c db.CompaniesCatalog) catalogResponse {
	return catalogResponse{
		ID:         c.ID,
		Name:       c.Name,
		CareersURL: c.CareersUrl,
		ProviderID: c.ProviderID,
		AtsAPIURL:  c.AtsApiUrl.String,
	}
}

// Servicer is the interface that handlers depend on.
type Servicer interface {
	List(ctx context.Context, userID uuid.UUID) ([]db.WatchedCompany, error)
	Add(ctx context.Context, userID uuid.UUID, name, careersURL, providerID string) (*db.WatchedCompany, error)
	AddFromCatalog(ctx context.Context, userID uuid.UUID, catalogID uuid.UUID) (*db.WatchedCompany, error)
	ListCatalog(ctx context.Context) ([]db.CompaniesCatalog, error)
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
	r.Get("/api/companies/catalog", h.Catalog)
	r.Post("/api/companies", h.Create)
	r.Delete("/api/companies/{id}", h.Delete)
}

// Catalog handles GET /api/companies/catalog — the global, install-wide list
// of companies a user can pick from. Not tenant-scoped.
func (h *Handler) Catalog(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.GetUserID(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing_user_id")
		return
	}

	catalog, err := h.svc.ListCatalog(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list catalog", "internal_error")
		return
	}

	resp := make([]catalogResponse, len(catalog))
	for i, c := range catalog {
		resp[i] = toCatalogResponse(c)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"catalog": resp,
	})
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

	resp := make([]companyResponse, len(companies))
	for i, c := range companies {
		resp[i] = toCompanyResponse(c)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"companies": resp,
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
		CatalogID  string `json:"catalog_id"`
		Name       string `json:"name"`
		CareersURL string `json:"careers_url"`
		ProviderID string `json:"provider_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "invalid_request")
		return
	}

	var company *db.WatchedCompany
	var err error

	// Catalog-driven add (curated, integrity-safe) takes precedence over manual
	// free-text entry. Manual entry remains for companies not in the catalog.
	if body.CatalogID != "" {
		catalogID, parseErr := uuid.Parse(body.CatalogID)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid catalog id", "invalid_request")
			return
		}
		company, err = h.svc.AddFromCatalog(r.Context(), userID, catalogID)
		if errors.Is(err, ErrCatalogNotFound) {
			writeError(w, http.StatusNotFound, "catalog entry not found", "not_found")
			return
		}
		if errors.Is(err, ErrAlreadyWatched) {
			writeError(w, http.StatusConflict, "company already watched", "already_watched")
			return
		}
	} else {
		if body.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required", "invalid_request")
			return
		}
		company, err = h.svc.Add(r.Context(), userID, body.Name, body.CareersURL, body.ProviderID)
	}

	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request")
		return
	}

	writeJSON(w, http.StatusCreated, toCompanyResponse(*company))
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
