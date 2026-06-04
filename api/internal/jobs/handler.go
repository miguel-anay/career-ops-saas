package jobs

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/middleware"
)

// Handler holds dependencies for jobs HTTP handlers.
type Handler struct {
	svc *Service
}

// NewHandler creates a new jobs Handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes mounts the jobs routes onto a chi.Router.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/jobs", h.List)
	r.Post("/api/jobs", h.Create)
	r.Get("/api/jobs/{id}", h.GetByID)
}

// List handles GET /api/jobs?page=1&limit=20
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

	jobList, err := h.svc.List(r.Context(), userID, page, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list jobs", "internal_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"jobs":  jobList,
		"page":  page,
		"limit": limit,
	})
}

// Create handles POST /api/jobs {url}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing_user_id")
		return
	}

	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required", "invalid_request")
		return
	}

	job, err := h.svc.AddManual(r.Context(), userID, body.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_url")
		return
	}

	platform := ""
	if job.Platform.Valid {
		platform = job.Platform.String
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":       job.ID,
		"url":      job.Url,
		"status":   job.Status,
		"platform": platform,
	})
}

// GetByID handles GET /api/jobs/{id}
func (h *Handler) GetByID(w http.ResponseWriter, r *http.Request) {
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

	job, err := h.svc.GetByID(r.Context(), userID, jobID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "job not found", "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get job", "internal_error")
		return
	}

	writeJSON(w, http.StatusOK, job)
}

// queryIntDefault parses an integer query param or returns the default value.
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
