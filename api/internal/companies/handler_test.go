package companies_test

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
	"github.com/miguel-anay/career-ops-saas/api/internal/companies"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
	"github.com/miguel-anay/career-ops-saas/api/internal/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockService struct {
	mock.Mock
}

func (m *MockService) List(ctx context.Context, userID uuid.UUID) ([]db.WatchedCompany, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]db.WatchedCompany), args.Error(1)
}

func (m *MockService) Add(ctx context.Context, userID uuid.UUID, name, careersURL, providerID string) (*db.WatchedCompany, error) {
	args := m.Called(ctx, userID, name, careersURL, providerID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*db.WatchedCompany), args.Error(1)
}

func (m *MockService) Remove(ctx context.Context, userID uuid.UUID, companyID uuid.UUID) error {
	args := m.Called(ctx, userID, companyID)
	return args.Error(0)
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
	h := companies.NewHandler(svc)

	userID := uuid.New()
	companyList := []db.WatchedCompany{
		{ID: uuid.New(), UserID: userID, Name: "Acme", Enabled: true, CreatedAt: time.Now()},
	}
	svc.On("List", mock.Anything, userID).Return(companyList, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/companies", nil)
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.List(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Contains(t, body, "companies")

	svc.AssertExpectations(t)
}

func TestList_MissingAuth(t *testing.T) {
	svc := &MockService{}
	h := companies.NewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/companies", nil)
	rec := httptest.NewRecorder()

	h.List(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	svc.AssertNotCalled(t, "List")
}

func TestList_ServiceError(t *testing.T) {
	svc := &MockService{}
	h := companies.NewHandler(svc)

	userID := uuid.New()
	svc.On("List", mock.Anything, userID).Return(nil, assert.AnError)

	req := httptest.NewRequest(http.MethodGet, "/api/companies", nil)
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.List(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	svc.AssertExpectations(t)
}

// ---- Create handler tests ----

func TestCreate_HappyPath(t *testing.T) {
	svc := &MockService{}
	h := companies.NewHandler(svc)

	userID := uuid.New()
	created := &db.WatchedCompany{
		ID:     uuid.New(),
		UserID: userID,
		Name:   "Acme",
		CareersUrl: sql.NullString{
			String: "https://greenhouse.io/acme",
			Valid:  true,
		},
		Enabled:   true,
		CreatedAt: time.Now(),
	}
	svc.On("Add", mock.Anything, userID, "Acme", "https://greenhouse.io/acme", "").Return(created, nil)

	body, _ := json.Marshal(map[string]string{"name": "Acme", "careers_url": "https://greenhouse.io/acme"})
	req := httptest.NewRequest(http.MethodPost, "/api/companies", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	svc.AssertExpectations(t)
}

func TestCreate_MissingAuth(t *testing.T) {
	svc := &MockService{}
	h := companies.NewHandler(svc)

	body, _ := json.Marshal(map[string]string{"name": "Acme"})
	req := httptest.NewRequest(http.MethodPost, "/api/companies", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestCreate_MissingName(t *testing.T) {
	svc := &MockService{}
	h := companies.NewHandler(svc)

	userID := uuid.New()
	body, _ := json.Marshal(map[string]string{})
	req := httptest.NewRequest(http.MethodPost, "/api/companies", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	svc.AssertNotCalled(t, "Add")
}

// ---- Delete handler tests ----

func TestDelete_HappyPath(t *testing.T) {
	svc := &MockService{}
	h := companies.NewHandler(svc)

	userID := uuid.New()
	companyID := uuid.New()
	svc.On("Remove", mock.Anything, userID, companyID).Return(nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/companies/"+companyID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": companyID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Delete(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	svc.AssertExpectations(t)
}

func TestDelete_NotFound(t *testing.T) {
	svc := &MockService{}
	h := companies.NewHandler(svc)

	userID := uuid.New()
	companyID := uuid.New()
	svc.On("Remove", mock.Anything, userID, companyID).Return(companies.ErrNotFound)

	req := httptest.NewRequest(http.MethodDelete, "/api/companies/"+companyID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": companyID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Delete(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	svc.AssertExpectations(t)
}

func TestDelete_MissingAuth(t *testing.T) {
	svc := &MockService{}
	h := companies.NewHandler(svc)

	companyID := uuid.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/companies/"+companyID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": companyID.String()})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Delete(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
