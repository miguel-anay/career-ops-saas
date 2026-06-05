package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/auth"
	"github.com/miguel-anay/career-ops-saas/api/internal/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSecret = "test-jwt-secret"

func TestAuthenticator_ValidToken(t *testing.T) {
	userID := uuid.New()
	token, err := auth.IssueAccessToken(userID.String(), "free", testSecret)
	require.NoError(t, err)

	var capturedUserID uuid.UUID
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := middleware.GetUserID(r.Context())
		require.True(t, ok, "userID must be in context")
		capturedUserID = id
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.Authenticator(testSecret)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, userID, capturedUserID)
}

func TestAuthenticator_MissingHeader(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.Authenticator(testSecret)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthenticator_MalformedHeader(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.Authenticator(testSecret)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Token abc123") // wrong scheme
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthenticator_InvalidToken(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.Authenticator(testSecret)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer not.a.valid.token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthenticator_WrongSecret(t *testing.T) {
	userID := uuid.New()
	token, err := auth.IssueAccessToken(userID.String(), "free", "wrong-secret")
	require.NoError(t, err)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.Authenticator(testSecret)(next) // expects different secret

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestSetUserID_GetUserID_RoundTrip(t *testing.T) {
	userID := uuid.New()
	ctx := middleware.SetUserID(req().Context(), userID)

	retrieved, ok := middleware.GetUserID(ctx)
	require.True(t, ok)
	assert.Equal(t, userID, retrieved)
}

func TestGetUserID_MissingFromContext(t *testing.T) {
	_, ok := middleware.GetUserID(req().Context())
	assert.False(t, ok)
}

func req() *http.Request {
	return httptest.NewRequest(http.MethodGet, "/", nil)
}
