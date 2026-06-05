package evaluate_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
	"github.com/miguel-anay/career-ops-saas/api/internal/evaluate"
	"github.com/miguel-anay/career-ops-saas/api/internal/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockService struct {
	mock.Mock
}

func (m *MockService) EnqueueEvaluation(ctx context.Context, userID, jobID uuid.UUID) (string, error) {
	args := m.Called(ctx, userID, jobID)
	return args.String(0), args.Error(1)
}

func (m *MockService) GetReport(ctx context.Context, userID, jobID uuid.UUID) (*db.Report, error) {
	args := m.Called(ctx, userID, jobID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*db.Report), args.Error(1)
}

func newChiCtx(params map[string]string) context.Context {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return context.WithValue(context.Background(), chi.RouteCtxKey, rctx)
}

// ---- Evaluate handler tests ----

func TestEvaluate_HappyPath(t *testing.T) {
	svc := &MockService{}
	h := evaluate.NewHandler(svc)

	userID := uuid.New()
	jobID := uuid.New()
	queueID := uuid.New().String()

	svc.On("EnqueueEvaluation", mock.Anything, userID, jobID).Return(queueID, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+jobID.String()+"/evaluate", nil)
	ctx := newChiCtx(map[string]string{"id": jobID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Evaluate(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, true, body["queued"])
	assert.Equal(t, queueID, body["queue_id"])

	svc.AssertExpectations(t)
}

func TestEvaluate_MissingAuth(t *testing.T) {
	svc := &MockService{}
	h := evaluate.NewHandler(svc)

	jobID := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+jobID.String()+"/evaluate", nil)
	ctx := newChiCtx(map[string]string{"id": jobID.String()})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Evaluate(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	svc.AssertNotCalled(t, "EnqueueEvaluation")
}

func TestEvaluate_InvalidID(t *testing.T) {
	svc := &MockService{}
	h := evaluate.NewHandler(svc)

	userID := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/not-a-uuid/evaluate", nil)
	ctx := newChiCtx(map[string]string{"id": "not-a-uuid"})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Evaluate(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestEvaluate_NotFound(t *testing.T) {
	svc := &MockService{}
	h := evaluate.NewHandler(svc)

	userID := uuid.New()
	jobID := uuid.New()
	svc.On("EnqueueEvaluation", mock.Anything, userID, jobID).Return("", evaluate.ErrNotFound)

	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+jobID.String()+"/evaluate", nil)
	ctx := newChiCtx(map[string]string{"id": jobID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Evaluate(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	svc.AssertExpectations(t)
}

func TestEvaluate_UsageLimitExceeded(t *testing.T) {
	svc := &MockService{}
	h := evaluate.NewHandler(svc)

	userID := uuid.New()
	jobID := uuid.New()
	svc.On("EnqueueEvaluation", mock.Anything, userID, jobID).Return("", evaluate.ErrUsageLimitExceeded)

	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+jobID.String()+"/evaluate", nil)
	ctx := newChiCtx(map[string]string{"id": jobID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Evaluate(rec, req)

	assert.Equal(t, http.StatusPaymentRequired, rec.Code)

	var body map[string]string
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "usage_limit_exceeded", body["code"])

	svc.AssertExpectations(t)
}

func TestEvaluate_ServiceError(t *testing.T) {
	svc := &MockService{}
	h := evaluate.NewHandler(svc)

	userID := uuid.New()
	jobID := uuid.New()
	svc.On("EnqueueEvaluation", mock.Anything, userID, jobID).Return("", assert.AnError)

	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+jobID.String()+"/evaluate", nil)
	ctx := newChiCtx(map[string]string{"id": jobID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Evaluate(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	svc.AssertExpectations(t)
}

// ---- GetReport handler tests ----

func TestGetReport_HappyPath(t *testing.T) {
	svc := &MockService{}
	h := evaluate.NewHandler(svc)

	userID := uuid.New()
	jobID := uuid.New()
	report := &db.Report{
		ID:         uuid.New(),
		UserID:     userID,
		ContentMd:  "# Report",
		BlocksJson: json.RawMessage(`{}`),
	}

	svc.On("GetReport", mock.Anything, userID, jobID).Return(report, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+jobID.String()+"/report", nil)
	ctx := newChiCtx(map[string]string{"id": jobID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetReport(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	svc.AssertExpectations(t)
}

func TestGetReport_MissingAuth(t *testing.T) {
	svc := &MockService{}
	h := evaluate.NewHandler(svc)

	jobID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+jobID.String()+"/report", nil)
	ctx := newChiCtx(map[string]string{"id": jobID.String()})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetReport(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetReport_NotFound(t *testing.T) {
	svc := &MockService{}
	h := evaluate.NewHandler(svc)

	userID := uuid.New()
	jobID := uuid.New()
	svc.On("GetReport", mock.Anything, userID, jobID).Return(nil, evaluate.ErrNotFound)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+jobID.String()+"/report", nil)
	ctx := newChiCtx(map[string]string{"id": jobID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetReport(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	svc.AssertExpectations(t)
}
