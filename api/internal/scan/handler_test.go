package scan_test

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
	"github.com/miguel-anay/career-ops-saas/api/internal/middleware"
	"github.com/miguel-anay/career-ops-saas/api/internal/scan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockService struct {
	mock.Mock
}

func (m *MockService) TriggerScan(ctx context.Context, userID uuid.UUID) (uuid.UUID, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).(uuid.UUID), args.Error(1)
}

func (m *MockService) GetScanRun(ctx context.Context, userID, scanRunID uuid.UUID) (*db.ScanRun, error) {
	args := m.Called(ctx, userID, scanRunID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*db.ScanRun), args.Error(1)
}

func newChiCtx(params map[string]string) context.Context {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return context.WithValue(context.Background(), chi.RouteCtxKey, rctx)
}

// ---- TriggerScan handler tests ----

func TestTriggerScan_HappyPath(t *testing.T) {
	svc := &MockService{}
	h := scan.NewHandler(svc)

	userID := uuid.New()
	scanRunID := uuid.New()
	svc.On("TriggerScan", mock.Anything, userID).Return(scanRunID, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/scan", nil)
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.TriggerScan(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, scanRunID.String(), body["scan_run_id"])

	svc.AssertExpectations(t)
}

func TestTriggerScan_MissingAuth(t *testing.T) {
	svc := &MockService{}
	h := scan.NewHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/scan", nil)
	rec := httptest.NewRecorder()

	h.TriggerScan(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	svc.AssertNotCalled(t, "TriggerScan")
}

func TestTriggerScan_ServiceError(t *testing.T) {
	svc := &MockService{}
	h := scan.NewHandler(svc)

	userID := uuid.New()
	svc.On("TriggerScan", mock.Anything, userID).Return(uuid.Nil, assert.AnError)

	req := httptest.NewRequest(http.MethodPost, "/api/scan", nil)
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.TriggerScan(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	svc.AssertExpectations(t)
}

// ---- GetScanRun handler tests ----

func TestGetScanRun_HappyPath(t *testing.T) {
	svc := &MockService{}
	h := scan.NewHandler(svc)

	userID := uuid.New()
	scanRunID := uuid.New()
	scanRun := &db.ScanRun{
		ID:        scanRunID,
		UserID:    userID,
		Status:    db.ScanStatusTCompleted,
		NewJobs:   5,
		StartedAt: time.Now(),
		FinishedAt: sql.NullTime{
			Time:  time.Now(),
			Valid: true,
		},
		ErrorsJson: json.RawMessage(`[]`),
	}
	svc.On("GetScanRun", mock.Anything, userID, scanRunID).Return(scanRun, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/scan-runs/"+scanRunID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": scanRunID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetScanRun(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	svc.AssertExpectations(t)
}

func TestGetScanRun_MissingAuth(t *testing.T) {
	svc := &MockService{}
	h := scan.NewHandler(svc)

	scanRunID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/scan-runs/"+scanRunID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": scanRunID.String()})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetScanRun(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetScanRun_InvalidID(t *testing.T) {
	svc := &MockService{}
	h := scan.NewHandler(svc)

	userID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/scan-runs/not-a-uuid", nil)
	ctx := newChiCtx(map[string]string{"id": "not-a-uuid"})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetScanRun(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetScanRun_NotFound(t *testing.T) {
	svc := &MockService{}
	h := scan.NewHandler(svc)

	userID := uuid.New()
	scanRunID := uuid.New()
	svc.On("GetScanRun", mock.Anything, userID, scanRunID).Return(nil, sql.ErrNoRows)

	req := httptest.NewRequest(http.MethodGet, "/api/scan-runs/"+scanRunID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": scanRunID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetScanRun(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	svc.AssertExpectations(t)
}

// TestGetScanRun_CrossTenant asserts the handler forwards the caller's user id
// to the service so cross-tenant lookups can be denied. User B requests user
// A's scan run by its exact id; the service reports it as not found (404), and
// the call must carry user B's id (proving userID is no longer discarded).
func TestGetScanRun_CrossTenant(t *testing.T) {
	svc := &MockService{}
	h := scan.NewHandler(svc)

	userB := uuid.New()
	scanRunID := uuid.New() // a scan run that belongs to user A in production
	svc.On("GetScanRun", mock.Anything, userB, scanRunID).Return(nil, sql.ErrNoRows)

	req := httptest.NewRequest(http.MethodGet, "/api/scan-runs/"+scanRunID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": scanRunID.String()})
	ctx = middleware.SetUserID(ctx, userB)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetScanRun(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code,
		"cross-tenant scan_run access must return 404, not reveal the row")
	svc.AssertExpectations(t)
}
