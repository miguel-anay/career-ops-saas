package digest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
	"github.com/miguel-anay/career-ops-saas/api/internal/digest"
	"github.com/miguel-anay/career-ops-saas/api/internal/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockService struct {
	mock.Mock
}

func (m *MockService) ListDigests(ctx context.Context, userID uuid.UUID) ([]db.ArticleDigest, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]db.ArticleDigest), args.Error(1)
}

func (m *MockService) CreateDigest(ctx context.Context, userID uuid.UUID, title, contentMd string) (*db.ArticleDigest, error) {
	args := m.Called(ctx, userID, title, contentMd)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*db.ArticleDigest), args.Error(1)
}

func (m *MockService) DeleteDigest(ctx context.Context, userID, digestID uuid.UUID) error {
	args := m.Called(ctx, userID, digestID)
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
	h := digest.NewHandler(svc)

	userID := uuid.New()
	digests := []db.ArticleDigest{
		{ID: uuid.New(), UserID: userID, Title: "Project A", ContentMd: "# hero metrics", CreatedAt: time.Now()},
	}
	svc.On("ListDigests", mock.Anything, userID).Return(digests, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/article-digests", nil)
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.List(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Contains(t, body, "digests")
	svc.AssertExpectations(t)
}

func TestList_MissingAuth(t *testing.T) {
	svc := &MockService{}
	h := digest.NewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/article-digests", nil)
	rec := httptest.NewRecorder()

	h.List(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	svc.AssertNotCalled(t, "ListDigests")
}

// ---- Create handler tests ----

func TestCreate_HappyPath(t *testing.T) {
	svc := &MockService{}
	h := digest.NewHandler(svc)

	userID := uuid.New()
	created := &db.ArticleDigest{ID: uuid.New(), UserID: userID, Title: "Project A", ContentMd: "# hero metrics", CreatedAt: time.Now()}
	svc.On("CreateDigest", mock.Anything, userID, "Project A", "# hero metrics").Return(created, nil)

	body, _ := json.Marshal(map[string]string{"title": "Project A", "content_md": "# hero metrics"})
	req := httptest.NewRequest(http.MethodPost, "/api/article-digests", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	svc.AssertExpectations(t)
}

func TestCreate_MissingAuth(t *testing.T) {
	svc := &MockService{}
	h := digest.NewHandler(svc)

	body, _ := json.Marshal(map[string]string{"title": "Project A", "content_md": "# hero metrics"})
	req := httptest.NewRequest(http.MethodPost, "/api/article-digests", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	svc.AssertNotCalled(t, "CreateDigest")
}

func TestCreate_EmptyTitleBubblesAs400(t *testing.T) {
	svc := &MockService{}
	h := digest.NewHandler(svc)

	userID := uuid.New()
	svc.On("CreateDigest", mock.Anything, userID, "", "# hero metrics").
		Return(nil, fmt.Errorf("title is required: %w", digest.ErrValidation))

	body, _ := json.Marshal(map[string]string{"title": "", "content_md": "# hero metrics"})
	req := httptest.NewRequest(http.MethodPost, "/api/article-digests", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	svc.AssertExpectations(t)
}

// Regression guard: a non-validation error (e.g. a DB failure inside
// CreateDigest's WithTenantTx) must map to 500, not 400 — Create previously
// mapped every error from the service to 400 via err.Error(), which would
// have misreported a genuine infra failure as a client input problem.
func TestCreate_NonValidationErrorBubblesAs500(t *testing.T) {
	svc := &MockService{}
	h := digest.NewHandler(svc)

	userID := uuid.New()
	svc.On("CreateDigest", mock.Anything, userID, "Title", "content").
		Return(nil, assert.AnError)

	body, _ := json.Marshal(map[string]string{"title": "Title", "content_md": "content"})
	req := httptest.NewRequest(http.MethodPost, "/api/article-digests", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(middleware.SetUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	svc.AssertExpectations(t)
}

// ---- Delete handler tests ----

func TestDelete_HappyPath(t *testing.T) {
	svc := &MockService{}
	h := digest.NewHandler(svc)

	userID := uuid.New()
	digestID := uuid.New()
	svc.On("DeleteDigest", mock.Anything, userID, digestID).Return(nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/article-digests/"+digestID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": digestID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Delete(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	svc.AssertExpectations(t)
}

func TestDelete_NotFound(t *testing.T) {
	svc := &MockService{}
	h := digest.NewHandler(svc)

	userID := uuid.New()
	digestID := uuid.New()
	svc.On("DeleteDigest", mock.Anything, userID, digestID).Return(digest.ErrNotFound)

	req := httptest.NewRequest(http.MethodDelete, "/api/article-digests/"+digestID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": digestID.String()})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Delete(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	svc.AssertExpectations(t)
}

func TestDelete_MissingAuth(t *testing.T) {
	svc := &MockService{}
	h := digest.NewHandler(svc)

	digestID := uuid.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/article-digests/"+digestID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": digestID.String()})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Delete(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	svc.AssertNotCalled(t, "DeleteDigest")
}

func TestDelete_MalformedID(t *testing.T) {
	svc := &MockService{}
	h := digest.NewHandler(svc)

	userID := uuid.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/article-digests/not-a-uuid", nil)
	ctx := newChiCtx(map[string]string{"id": "not-a-uuid"})
	ctx = middleware.SetUserID(ctx, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Delete(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	svc.AssertNotCalled(t, "DeleteDigest")
}
