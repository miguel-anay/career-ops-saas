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

func (m *MockService) EnqueueIngest(ctx context.Context, userID uuid.UUID, rawCV string) (uuid.UUID, error) {
	args := m.Called(ctx, userID, rawCV)
	return args.Get(0).(uuid.UUID), args.Error(1)
}

func (m *MockService) GetIngestion(ctx context.Context, userID, runID uuid.UUID) (*db.CvIngestion, error) {
	args := m.Called(ctx, userID, runID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*db.CvIngestion), args.Error(1)
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

// ---- Ingest handler tests (T-89) ----

func TestIngest_HappyPath(t *testing.T) {
	svc := &MockService{}
	h := cv.NewHandler(svc, nil)

	userID := uuid.New()
	runID := uuid.New()
	svc.On("EnqueueIngest", mock.Anything, userID, "some raw cv text").Return(runID, nil)

	body, _ := json.Marshal(map[string]string{"raw_cv": "some raw cv text"})
	req := httptest.NewRequest(http.MethodPost, "/api/cv/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Ingest(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	var respBody map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&respBody))
	assert.Equal(t, runID.String(), respBody["run_id"])
	svc.AssertExpectations(t)
}

func TestIngest_MissingAuth(t *testing.T) {
	svc := &MockService{}
	h := cv.NewHandler(svc, nil)

	body, _ := json.Marshal(map[string]string{"raw_cv": "some raw cv text"})
	req := httptest.NewRequest(http.MethodPost, "/api/cv/ingest", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Ingest(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	svc.AssertNotCalled(t, "EnqueueIngest")
}

func TestIngest_EmptyBody(t *testing.T) {
	svc := &MockService{}
	h := cv.NewHandler(svc, nil)

	userID := uuid.New()
	body, _ := json.Marshal(map[string]string{"raw_cv": ""})
	req := httptest.NewRequest(http.MethodPost, "/api/cv/ingest", bytes.NewReader(body))
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Ingest(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	svc.AssertNotCalled(t, "EnqueueIngest")
}

func TestIngest_WhitespaceOnlyBody(t *testing.T) {
	svc := &MockService{}
	h := cv.NewHandler(svc, nil)

	userID := uuid.New()
	body, _ := json.Marshal(map[string]string{"raw_cv": "   \n\t  "})
	req := httptest.NewRequest(http.MethodPost, "/api/cv/ingest", bytes.NewReader(body))
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Ingest(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	svc.AssertNotCalled(t, "EnqueueIngest")
}

func TestIngest_MissingField(t *testing.T) {
	svc := &MockService{}
	h := cv.NewHandler(svc, nil)

	userID := uuid.New()
	body, _ := json.Marshal(map[string]string{})
	req := httptest.NewRequest(http.MethodPost, "/api/cv/ingest", bytes.NewReader(body))
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Ingest(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	svc.AssertNotCalled(t, "EnqueueIngest")
}

func TestIngest_OversizedBody(t *testing.T) {
	svc := &MockService{}
	h := cv.NewHandler(svc, nil)

	userID := uuid.New()
	// maxRawCVLength is 100KB (per design decision); exceed it.
	oversized := make([]byte, 100*1024+1)
	for i := range oversized {
		oversized[i] = 'a'
	}
	body, _ := json.Marshal(map[string]string{"raw_cv": string(oversized)})
	req := httptest.NewRequest(http.MethodPost, "/api/cv/ingest", bytes.NewReader(body))
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Ingest(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	svc.AssertNotCalled(t, "EnqueueIngest")
}

func TestIngest_UsageLimitExceeded(t *testing.T) {
	svc := &MockService{}
	h := cv.NewHandler(svc, nil)

	userID := uuid.New()
	svc.On("EnqueueIngest", mock.Anything, userID, "some raw cv text").Return(uuid.Nil, cv.ErrUsageLimitExceeded)

	body, _ := json.Marshal(map[string]string{"raw_cv": "some raw cv text"})
	req := httptest.NewRequest(http.MethodPost, "/api/cv/ingest", bytes.NewReader(body))
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Ingest(rec, req)

	assert.Equal(t, http.StatusPaymentRequired, rec.Code)
	svc.AssertExpectations(t)
}

func TestIngest_ServiceInternalError(t *testing.T) {
	svc := &MockService{}
	h := cv.NewHandler(svc, nil)

	userID := uuid.New()
	svc.On("EnqueueIngest", mock.Anything, userID, "some raw cv text").Return(uuid.Nil, assert.AnError)

	body, _ := json.Marshal(map[string]string{"raw_cv": "some raw cv text"})
	req := httptest.NewRequest(http.MethodPost, "/api/cv/ingest", bytes.NewReader(body))
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Ingest(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	svc.AssertExpectations(t)
}

// ---- GetIngestion handler tests (T-91) ----

func TestGetIngestion_HappyPath(t *testing.T) {
	svc := &MockService{}
	h := cv.NewHandler(svc, nil)

	userID := uuid.New()
	runID := uuid.New()
	ingestion := &db.CvIngestion{
		ID:        runID,
		UserID:    userID,
		Status:    "pending",
		StartedAt: time.Now(),
	}
	svc.On("GetIngestion", mock.Anything, userID, runID).Return(ingestion, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/cv/ingest/"+runID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": runID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetIngestion(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, runID.String(), body["id"])
	assert.Equal(t, "pending", body["status"])
	svc.AssertExpectations(t)
}

func TestGetIngestion_NotFound(t *testing.T) {
	svc := &MockService{}
	h := cv.NewHandler(svc, nil)

	userID := uuid.New()
	runID := uuid.New()
	svc.On("GetIngestion", mock.Anything, userID, runID).Return(nil, cv.ErrNotFound)

	req := httptest.NewRequest(http.MethodGet, "/api/cv/ingest/"+runID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": runID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetIngestion(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	svc.AssertExpectations(t)
}

func TestGetIngestion_NonOwnerTreatedAsNotFound(t *testing.T) {
	svc := &MockService{}
	h := cv.NewHandler(svc, nil)

	userB := uuid.New()
	runID := uuid.New() // owned by user A, but RLS makes it invisible to B
	svc.On("GetIngestion", mock.Anything, userB, runID).Return(nil, cv.ErrNotFound)

	req := httptest.NewRequest(http.MethodGet, "/api/cv/ingest/"+runID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": runID.String()})
	ctx = middleware.SetUserID(ctx, userB)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetIngestion(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	svc.AssertExpectations(t)
}

func TestGetIngestion_MalformedID(t *testing.T) {
	svc := &MockService{}
	h := cv.NewHandler(svc, nil)

	userID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/cv/ingest/not-a-uuid", nil)
	ctx := newChiCtx(map[string]string{"id": "not-a-uuid"})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetIngestion(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	svc.AssertNotCalled(t, "GetIngestion")
}

func TestGetIngestion_MissingAuth(t *testing.T) {
	svc := &MockService{}
	h := cv.NewHandler(svc, nil)

	runID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/cv/ingest/"+runID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": runID.String()})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetIngestion(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	svc.AssertNotCalled(t, "GetIngestion")
}
