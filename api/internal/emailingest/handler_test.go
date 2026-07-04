package emailingest_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
	"github.com/miguel-anay/career-ops-saas/api/internal/emailingest"
	"github.com/miguel-anay/career-ops-saas/api/internal/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockService struct {
	mock.Mock
}

func (m *MockService) TriggerIngest(ctx context.Context, userID uuid.UUID) (uuid.UUID, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).(uuid.UUID), args.Error(1)
}

func (m *MockService) GetIngestRun(ctx context.Context, userID, ingestRunID uuid.UUID) (*db.EmailIngestRun, error) {
	args := m.Called(ctx, userID, ingestRunID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*db.EmailIngestRun), args.Error(1)
}

func newChiCtx(params map[string]string) context.Context {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return context.WithValue(context.Background(), chi.RouteCtxKey, rctx)
}

// ---- POST /api/email-ingest handler tests ----

func TestTriggerIngest_HappyPath(t *testing.T) {
	svc := &MockService{}
	h := emailingest.NewHandler(svc)

	userID := uuid.New()
	runID := uuid.New()
	svc.On("TriggerIngest", mock.Anything, userID).Return(runID, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/email-ingest", nil)
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.TriggerIngest(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, runID.String(), body["ingest_run_id"])

	svc.AssertExpectations(t)
}

func TestTriggerIngest_MissingAuth(t *testing.T) {
	svc := &MockService{}
	h := emailingest.NewHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/email-ingest", nil)
	rec := httptest.NewRecorder()

	h.TriggerIngest(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	svc.AssertNotCalled(t, "TriggerIngest")
}

// TestTriggerIngest_GmailNotConnected proves spec scenario "Ingest attempted
// without Gmail token": POST /api/email-ingest returns 422 with
// {"error":"gmail_not_connected"} and creates no run.
func TestTriggerIngest_GmailNotConnected(t *testing.T) {
	svc := &MockService{}
	h := emailingest.NewHandler(svc)

	userID := uuid.New()
	svc.On("TriggerIngest", mock.Anything, userID).Return(uuid.Nil, emailingest.ErrGmailNotConnected)

	req := httptest.NewRequest(http.MethodPost, "/api/email-ingest", nil)
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.TriggerIngest(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "gmail_not_connected", body["error"])

	svc.AssertExpectations(t)
}

func TestTriggerIngest_ServiceError(t *testing.T) {
	svc := &MockService{}
	h := emailingest.NewHandler(svc)

	userID := uuid.New()
	svc.On("TriggerIngest", mock.Anything, userID).Return(uuid.Nil, assert.AnError)

	req := httptest.NewRequest(http.MethodPost, "/api/email-ingest", nil)
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.TriggerIngest(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	svc.AssertExpectations(t)
}

// ---- GET /api/email-ingest-runs/{id} handler tests ----

func TestGetIngestRun_HappyPath(t *testing.T) {
	svc := &MockService{}
	h := emailingest.NewHandler(svc)

	userID := uuid.New()
	runID := uuid.New()
	run := &db.EmailIngestRun{
		ID:        runID,
		UserID:    userID,
		Status:    "completed",
		NewJobs:   3,
		StartedAt: time.Now(),
		FinishedAt: sql.NullTime{
			Time:  time.Now(),
			Valid: true,
		},
		ErrorsJson: json.RawMessage(`[]`),
	}
	svc.On("GetIngestRun", mock.Anything, userID, runID).Return(run, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/email-ingest-runs/"+runID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": runID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetIngestRun(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	svc.AssertExpectations(t)
}

func TestGetIngestRun_MissingAuth(t *testing.T) {
	svc := &MockService{}
	h := emailingest.NewHandler(svc)

	runID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/email-ingest-runs/"+runID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": runID.String()})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetIngestRun(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetIngestRun_InvalidID(t *testing.T) {
	svc := &MockService{}
	h := emailingest.NewHandler(svc)

	userID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/email-ingest-runs/not-a-uuid", nil)
	ctx := newChiCtx(map[string]string{"id": "not-a-uuid"})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetIngestRun(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestGetIngestRun_NotFound_NonOwner proves the 404 non-owner scenario from
// T-230: a run that belongs to another tenant is reported as not found.
func TestGetIngestRun_NotFound_NonOwner(t *testing.T) {
	svc := &MockService{}
	h := emailingest.NewHandler(svc)

	userB := uuid.New()
	runID := uuid.New() // belongs to user A in production
	svc.On("GetIngestRun", mock.Anything, userB, runID).Return(nil, sql.ErrNoRows)

	req := httptest.NewRequest(http.MethodGet, "/api/email-ingest-runs/"+runID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": runID.String()})
	ctx = middleware.SetUserID(ctx, userB)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetIngestRun(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code,
		"cross-tenant email_ingest_run access must return 404, not reveal the row")
	svc.AssertExpectations(t)
}
