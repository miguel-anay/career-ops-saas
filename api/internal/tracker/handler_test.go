package tracker_test

import (
	"bytes"
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
	"github.com/miguel-anay/career-ops-saas/api/internal/tracker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockService struct {
	mock.Mock
}

func (m *MockService) ListApplications(ctx context.Context, userID uuid.UUID, page, limit int) ([]db.Application, error) {
	args := m.Called(ctx, userID, page, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]db.Application), args.Error(1)
}

func (m *MockService) UpdateApplication(ctx context.Context, userID, appID uuid.UUID, status *string, notes *string) (*db.Application, error) {
	args := m.Called(ctx, userID, appID, status, notes)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*db.Application), args.Error(1)
}

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
	h := tracker.NewHandler(svc)

	userID := uuid.New()
	apps := []db.Application{
		{ID: uuid.New(), UserID: userID, Status: db.AppStatusTEvaluated, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}
	svc.On("ListApplications", mock.Anything, userID, 1, 20).Return(apps, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/applications", nil)
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.List(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Contains(t, body, "applications")

	svc.AssertExpectations(t)
}

func TestList_MissingAuth(t *testing.T) {
	svc := &MockService{}
	h := tracker.NewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/applications", nil)
	rec := httptest.NewRecorder()

	h.List(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	svc.AssertNotCalled(t, "ListApplications")
}

func TestList_ServiceError(t *testing.T) {
	svc := &MockService{}
	h := tracker.NewHandler(svc)

	userID := uuid.New()
	svc.On("ListApplications", mock.Anything, userID, 1, 20).Return(nil, assert.AnError)

	req := httptest.NewRequest(http.MethodGet, "/api/applications", nil)
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.List(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	svc.AssertExpectations(t)
}

// ---- Update handler tests ----

func TestUpdate_HappyPath_StatusOnly(t *testing.T) {
	svc := &MockService{}
	h := tracker.NewHandler(svc)

	userID := uuid.New()
	appID := uuid.New()
	status := "Applied"
	updated := &db.Application{
		ID:        appID,
		UserID:    userID,
		Status:    db.AppStatusTApplied,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	svc.On("UpdateApplication", mock.Anything, userID, appID, &status, (*string)(nil)).Return(updated, nil)

	body, _ := json.Marshal(map[string]interface{}{"status": status})
	req := httptest.NewRequest(http.MethodPatch, "/api/applications/"+appID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := newChiCtx(map[string]string{"id": appID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	svc.AssertExpectations(t)
}

func TestUpdate_MissingAuth(t *testing.T) {
	svc := &MockService{}
	h := tracker.NewHandler(svc)

	appID := uuid.New()
	body, _ := json.Marshal(map[string]string{"status": "Applied"})
	req := httptest.NewRequest(http.MethodPatch, "/api/applications/"+appID.String(), bytes.NewReader(body))
	ctx := newChiCtx(map[string]string{"id": appID.String()})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestUpdate_InvalidID(t *testing.T) {
	svc := &MockService{}
	h := tracker.NewHandler(svc)

	userID := uuid.New()
	body, _ := json.Marshal(map[string]string{"status": "Applied"})
	req := httptest.NewRequest(http.MethodPatch, "/api/applications/not-a-uuid", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := newChiCtx(map[string]string{"id": "not-a-uuid"})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUpdate_NoFields(t *testing.T) {
	svc := &MockService{}
	h := tracker.NewHandler(svc)

	userID := uuid.New()
	appID := uuid.New()
	body, _ := json.Marshal(map[string]interface{}{})
	req := httptest.NewRequest(http.MethodPatch, "/api/applications/"+appID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := newChiCtx(map[string]string{"id": appID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	svc.AssertNotCalled(t, "UpdateApplication")
}

func TestUpdate_InvalidStatus(t *testing.T) {
	svc := &MockService{}
	h := tracker.NewHandler(svc)

	userID := uuid.New()
	appID := uuid.New()
	invalidStatus := "InvalidStatus"
	svc.On("UpdateApplication", mock.Anything, userID, appID, &invalidStatus, (*string)(nil)).Return(nil, tracker.ErrInvalidStatus)

	body, _ := json.Marshal(map[string]interface{}{"status": invalidStatus})
	req := httptest.NewRequest(http.MethodPatch, "/api/applications/"+appID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := newChiCtx(map[string]string{"id": appID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	svc.AssertExpectations(t)
}

func TestUpdate_NotFound(t *testing.T) {
	svc := &MockService{}
	h := tracker.NewHandler(svc)

	userID := uuid.New()
	appID := uuid.New()
	status := "Applied"
	svc.On("UpdateApplication", mock.Anything, userID, appID, &status, (*string)(nil)).Return(nil, tracker.ErrNotFound)

	body, _ := json.Marshal(map[string]interface{}{"status": status})
	req := httptest.NewRequest(http.MethodPatch, "/api/applications/"+appID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := newChiCtx(map[string]string{"id": appID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	svc.AssertExpectations(t)
}

// TestList_SerializesNullablesAsPlainJSON pins the wire contract the tracker
// page expects (app.score.toFixed, app.pdf_path href): nullable columns must
// serialize as plain values or null, never sql.Null* wrapper objects — those
// crash React (issue #40, same family as the jobs endpoints).
func TestList_SerializesNullablesAsPlainJSON(t *testing.T) {
	svc := &MockService{}
	h := tracker.NewHandler(svc)

	userID := uuid.New()
	apps := []db.Application{{
		ID:        uuid.New(),
		UserID:    userID,
		JobID:     uuid.New(),
		Score:     sql.NullFloat64{Float64: 4.2, Valid: true},
		Status:    db.AppStatusTEvaluated,
		Notes:     sql.NullString{},
		PdfPath:   sql.NullString{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}}
	svc.On("ListApplications", mock.Anything, userID, 1, 20).Return(apps, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/applications", nil)
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.List(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Applications []map[string]interface{} `json:"applications"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Len(t, body.Applications, 1)
	assert.Equal(t, 4.2, body.Applications[0]["score"])
	assert.Nil(t, body.Applications[0]["notes"])
	assert.Nil(t, body.Applications[0]["pdf_path"])

	svc.AssertExpectations(t)
}
