package jobs_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
	"github.com/miguel-anay/career-ops-saas/api/internal/jobs"
	"github.com/miguel-anay/career-ops-saas/api/internal/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockService is a testify mock for jobs.Servicer.
type MockService struct {
	mock.Mock
}

func (m *MockService) AddManual(ctx context.Context, userID uuid.UUID, rawURL string) (*db.Job, error) {
	args := m.Called(ctx, userID, rawURL)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*db.Job), args.Error(1)
}

func (m *MockService) List(ctx context.Context, userID uuid.UUID, page, limit int) ([]db.Job, error) {
	args := m.Called(ctx, userID, page, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]db.Job), args.Error(1)
}

func (m *MockService) GetByID(ctx context.Context, userID uuid.UUID, jobID uuid.UUID) (*db.Job, error) {
	args := m.Called(ctx, userID, jobID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*db.Job), args.Error(1)
}

// newChiContext returns a chi router context with the given URL params set,
// used to simulate URL params parsed by chi middleware.
func newChiCtx(params map[string]string) context.Context {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return context.WithValue(context.Background(), chi.RouteCtxKey, rctx)
}

// ---- List handler tests ----

func TestList_HappyPath(t *testing.T) {
	svc := &MockService{}
	h := jobs.NewHandler(svc)

	userID := uuid.New()
	expectedJobs := []db.Job{
		{ID: uuid.New(), UserID: userID, Title: "Engineer", Company: "Acme", Url: "https://example.com/job/1", Status: db.JobStatusTNew},
	}

	svc.On("List", mock.Anything, userID, 1, 20).Return(expectedJobs, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.List(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Contains(t, body, "jobs")

	svc.AssertExpectations(t)
}

func TestList_MissingAuth(t *testing.T) {
	svc := &MockService{}
	h := jobs.NewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	rec := httptest.NewRecorder()

	h.List(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	svc.AssertNotCalled(t, "List")
}

func TestList_ServiceError(t *testing.T) {
	svc := &MockService{}
	h := jobs.NewHandler(svc)

	userID := uuid.New()
	svc.On("List", mock.Anything, userID, 1, 20).Return(nil, assert.AnError)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.List(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	svc.AssertExpectations(t)
}

// ---- Create handler tests ----

func TestCreate_HappyPath(t *testing.T) {
	svc := &MockService{}
	h := jobs.NewHandler(svc)

	userID := uuid.New()
	rawURL := "https://greenhouse.io/apply/123"
	createdJob := &db.Job{
		ID:     uuid.New(),
		UserID: userID,
		Url:    rawURL,
		Status: db.JobStatusTNew,
		Platform: sql.NullString{
			String: "greenhouse",
			Valid:  true,
		},
	}

	svc.On("AddManual", mock.Anything, userID, rawURL).Return(createdJob, nil)

	body, _ := json.Marshal(map[string]string{"url": rawURL})
	req := httptest.NewRequest(http.MethodPost, "/api/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, rawURL, resp["url"])
	assert.Equal(t, "greenhouse", resp["platform"])

	svc.AssertExpectations(t)
}

func TestCreate_MissingAuth(t *testing.T) {
	svc := &MockService{}
	h := jobs.NewHandler(svc)

	body, _ := json.Marshal(map[string]string{"url": "https://example.com/job"})
	req := httptest.NewRequest(http.MethodPost, "/api/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestCreate_MissingURL(t *testing.T) {
	svc := &MockService{}
	h := jobs.NewHandler(svc)

	userID := uuid.New()
	body, _ := json.Marshal(map[string]string{})
	req := httptest.NewRequest(http.MethodPost, "/api/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	svc.AssertNotCalled(t, "AddManual")
}

func TestCreate_ServiceError(t *testing.T) {
	svc := &MockService{}
	h := jobs.NewHandler(svc)

	userID := uuid.New()
	rawURL := "https://example.com/job"
	svc.On("AddManual", mock.Anything, userID, rawURL).Return(nil, assert.AnError)

	body, _ := json.Marshal(map[string]string{"url": rawURL})
	req := httptest.NewRequest(http.MethodPost, "/api/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	svc.AssertExpectations(t)
}

// ---- GetByID handler tests ----

func TestGetByID_HappyPath(t *testing.T) {
	svc := &MockService{}
	h := jobs.NewHandler(svc)

	userID := uuid.New()
	jobID := uuid.New()
	expectedJob := &db.Job{ID: jobID, UserID: userID, Title: "Engineer", Company: "Acme", Url: "https://example.com/job/1", Status: db.JobStatusTNew}

	svc.On("GetByID", mock.Anything, userID, jobID).Return(expectedJob, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+jobID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": jobID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetByID(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	svc.AssertExpectations(t)
}

func TestGetByID_MissingAuth(t *testing.T) {
	svc := &MockService{}
	h := jobs.NewHandler(svc)

	jobID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+jobID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": jobID.String()})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetByID(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetByID_InvalidID(t *testing.T) {
	svc := &MockService{}
	h := jobs.NewHandler(svc)

	userID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/not-a-uuid", nil)
	ctx := newChiCtx(map[string]string{"id": "not-a-uuid"})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetByID(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetByID_NotFound(t *testing.T) {
	svc := &MockService{}
	h := jobs.NewHandler(svc)

	userID := uuid.New()
	jobID := uuid.New()
	svc.On("GetByID", mock.Anything, userID, jobID).Return(nil, jobs.ErrNotFound)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+jobID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": jobID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetByID(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	svc.AssertExpectations(t)
}

func TestGetByID_ServiceError(t *testing.T) {
	svc := &MockService{}
	h := jobs.NewHandler(svc)

	userID := uuid.New()
	jobID := uuid.New()
	svc.On("GetByID", mock.Anything, userID, jobID).Return(nil, assert.AnError)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+jobID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": jobID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetByID(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	svc.AssertExpectations(t)
}

// TestGetByID_SerializesNullablesAsPlainJSON pins the wire contract the web
// expects (web/features/jobs/types.ts: platform string, received_at string):
// nullable columns must serialize as plain values or null, never as Go
// wrapper objects like {"String":...,"Valid":...} — those crash React when
// rendered (issue #40).
func TestGetByID_SerializesNullablesAsPlainJSON(t *testing.T) {
	svc := &MockService{}
	h := jobs.NewHandler(svc)

	userID := uuid.New()
	jobID := uuid.New()
	job := &db.Job{
		ID:       jobID,
		UserID:   userID,
		Title:    "Engineer",
		Company:  "Acme",
		Url:      "https://example.com/job/1",
		Status:   db.JobStatusTNew,
		Platform: sql.NullString{String: "linkedin", Valid: true},
	}

	svc.On("GetByID", mock.Anything, userID, jobID).Return(job, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+jobID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": jobID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetByID(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "linkedin", resp["platform"])
	assert.Nil(t, resp["scraped_content"])
	assert.Nil(t, resp["received_at"])

	svc.AssertExpectations(t)
}

// TestList_SerializesNullablesAsPlainJSON: same wire contract for the list
// endpoint (the dashboard calls formatDate(job.received_at)).
func TestList_SerializesNullablesAsPlainJSON(t *testing.T) {
	svc := &MockService{}
	h := jobs.NewHandler(svc)

	userID := uuid.New()
	expectedJobs := []db.Job{{
		ID:       uuid.New(),
		UserID:   userID,
		Title:    "Engineer",
		Company:  "Acme",
		Url:      "https://example.com/job/1",
		Status:   db.JobStatusTNew,
		Platform: sql.NullString{String: "greenhouse", Valid: true},
	}}

	svc.On("List", mock.Anything, userID, 1, 20).Return(expectedJobs, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.List(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Jobs []map[string]interface{} `json:"jobs"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Len(t, body.Jobs, 1)
	assert.Equal(t, "greenhouse", body.Jobs[0]["platform"])
	assert.Nil(t, body.Jobs[0]["received_at"])

	svc.AssertExpectations(t)
}
