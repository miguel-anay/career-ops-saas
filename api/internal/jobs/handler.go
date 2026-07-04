package jobs

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
// The concrete *Service satisfies this automatically.
type Servicer interface {
	AddManual(ctx context.Context, userID uuid.UUID, rawURL string) (*db.Job, error)
	List(ctx context.Context, userID uuid.UUID, page, limit int) ([]db.Job, error)
	GetByID(ctx context.Context, userID uuid.UUID, jobID uuid.UUID) (*db.Job, error)
}

// Handler holds dependencies for jobs HTTP handlers.
type Handler struct {
	svc Servicer
}

// NewHandler creates a new jobs Handler.
func NewHandler(svc Servicer) *Handler {
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
	jobsOut := make([]map[string]interface{}, 0, len(jobList))
	for _, j := range jobList {
		jobsOut = append(jobsOut, jobJSON(j))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"jobs":  jobsOut,
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

	writeJSON(w, http.StatusCreated, jobJSON(*job))
}

// jobJSON maps a db.Job to the wire shape the web expects
// (web/features/jobs/types.ts): nullable columns as plain values or null,
// never sql.Null* wrapper objects — rendering {"String":...,"Valid":...}
// crashes React (issue #40).
func jobJSON(j db.Job) map[string]interface{} {
	out := map[string]interface{}{
		"id":              j.ID,
		"user_id":         j.UserID,
		"title":           j.Title,
		"company":         j.Company,
		"url":             j.Url,
		"status":          j.Status,
		"created_at":      j.CreatedAt,
		"platform":        nil,
		"scraped_content": nil,
		"received_at":     nil,
		"evaluation_json": nil,
	}
	if j.Platform.Valid {
		out["platform"] = j.Platform.String
	}
	if j.ScrapedContent.Valid {
		out["scraped_content"] = j.ScrapedContent.String
	}
	if j.ReceivedAt.Valid {
		out["received_at"] = j.ReceivedAt.Time
	}
	if j.EvaluationJson.Valid {
		out["evaluation_json"] = json.RawMessage(j.EvaluationJson.RawMessage)
	}
	return out
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

	writeJSON(w, http.StatusOK, jobJSON(*job))
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
