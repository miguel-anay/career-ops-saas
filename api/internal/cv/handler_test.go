package cv_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/cv"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
	"github.com/miguel-anay/career-ops-saas/api/internal/middleware"
	"github.com/miguel-anay/career-ops-saas/api/internal/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockService struct {
	mock.Mock
}

func (m *MockService) EnqueuePDFGeneration(ctx context.Context, userID, jobID uuid.UUID) (string, error) {
	args := m.Called(ctx, userID, jobID)
	return args.String(0), args.Error(1)
}

func (m *MockService) GetDownloadURL(ctx context.Context, r2 *platform.R2Client, userID, jobID uuid.UUID) (string, time.Time, error) {
	args := m.Called(ctx, r2, userID, jobID)
	return args.String(0), args.Get(1).(time.Time), args.Error(2)
}

func (m *MockService) ListCVs(ctx context.Context, userID uuid.UUID) ([]db.Cv, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]db.Cv), args.Error(1)
}

func (m *MockService) CreateCV(ctx context.Context, userID uuid.UUID, title, contentMd string, isMaster bool) (*db.Cv, error) {
	args := m.Called(ctx, userID, title, contentMd, isMaster)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*db.Cv), args.Error(1)
}

func newChiCtx(params map[string]string) context.Context {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return context.WithValue(context.Background(), chi.RouteCtxKey, rctx)
}

// ---- EnqueueCV handler tests ----

func TestEnqueueCV_HappyPath(t *testing.T) {
	svc := &MockService{}
	h := cv.NewHandler(svc, nil)

	userID := uuid.New()
	jobID := uuid.New()
	queueID := uuid.New().String()
	svc.On("EnqueuePDFGeneration", mock.Anything, userID, jobID).Return(queueID, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+jobID.String()+"/cv", nil)
	ctx := newChiCtx(map[string]string{"id": jobID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.EnqueueCV(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, true, body["queued"])

	svc.AssertExpectations(t)
}

func TestEnqueueCV_MissingAuth(t *testing.T) {
	svc := &MockService{}
	h := cv.NewHandler(svc, nil)

	jobID := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+jobID.String()+"/cv", nil)
	ctx := newChiCtx(map[string]string{"id": jobID.String()})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.EnqueueCV(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestEnqueueCV_NotFound(t *testing.T) {
	svc := &MockService{}
	h := cv.NewHandler(svc, nil)

	userID := uuid.New()
	jobID := uuid.New()
	svc.On("EnqueuePDFGeneration", mock.Anything, userID, jobID).Return("", cv.ErrNotFound)

	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+jobID.String()+"/cv", nil)
	ctx := newChiCtx(map[string]string{"id": jobID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.EnqueueCV(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	svc.AssertExpectations(t)
}

// ---- GetCV handler tests ----

func TestGetCV_R2NotConfigured(t *testing.T) {
	svc := &MockService{}
	h := cv.NewHandler(svc, nil) // nil R2 client

	userID := uuid.New()
	jobID := uuid.New()

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+jobID.String()+"/cv", nil)
	ctx := newChiCtx(map[string]string{"id": jobID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetCV(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestGetCV_MissingAuth(t *testing.T) {
	svc := &MockService{}
	h := cv.NewHandler(svc, nil)

	jobID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+jobID.String()+"/cv", nil)
	ctx := newChiCtx(map[string]string{"id": jobID.String()})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetCV(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// ---- ListCVs handler tests ----

func TestListCVs_HappyPath(t *testing.T) {
	svc := &MockService{}
	h := cv.NewHandler(svc, nil)

	userID := uuid.New()
	cvList := []db.Cv{
		{ID: uuid.New(), UserID: userID, Title: "My CV", ContentMd: "# Hello", CreatedAt: time.Now()},
	}
	svc.On("ListCVs", mock.Anything, userID).Return(cvList, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/cvs", nil)
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.ListCVs(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Contains(t, body, "cvs")

	svc.AssertExpectations(t)
}

func TestListCVs_MissingAuth(t *testing.T) {
	svc := &MockService{}
	h := cv.NewHandler(svc, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/cvs", nil)
	rec := httptest.NewRecorder()

	h.ListCVs(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// ---- CreateCV handler tests ----

func TestCreateCV_HappyPath(t *testing.T) {
	svc := &MockService{}
	h := cv.NewHandler(svc, nil)

	userID := uuid.New()
	created := &db.Cv{ID: uuid.New(), UserID: userID, Title: "My CV", ContentMd: "# Hello", CreatedAt: time.Now()}
	svc.On("CreateCV", mock.Anything, userID, "My CV", "# Hello", false).Return(created, nil)

	body, _ := json.Marshal(map[string]interface{}{"title": "My CV", "content_md": "# Hello", "is_master": false})
	req := httptest.NewRequest(http.MethodPost, "/api/cvs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.CreateCV(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	svc.AssertExpectations(t)
}

func TestCreateCV_MissingAuth(t *testing.T) {
	svc := &MockService{}
	h := cv.NewHandler(svc, nil)

	body, _ := json.Marshal(map[string]string{"title": "My CV", "content_md": "# Hello"})
	req := httptest.NewRequest(http.MethodPost, "/api/cvs", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.CreateCV(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestCreateCV_ServiceError(t *testing.T) {
	svc := &MockService{}
	h := cv.NewHandler(svc, nil)

	userID := uuid.New()
	svc.On("CreateCV", mock.Anything, userID, "My CV", "# Hello", false).Return(nil, assert.AnError)

	body, _ := json.Marshal(map[string]interface{}{"title": "My CV", "content_md": "# Hello", "is_master": false})
	req := httptest.NewRequest(http.MethodPost, "/api/cvs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.CreateCV(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	svc.AssertExpectations(t)
}
