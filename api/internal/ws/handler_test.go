package ws_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/ws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testJWTSecret = "test-secret-32-bytes-long-enough!!"

// signToken creates a signed HS256 JWT with the given expiry delta.
func signToken(t *testing.T, userID string, expiry time.Duration) string {
	t.Helper()
	claims := jwt.MapClaims{
		"user_id": userID,
		"plan":    "free",
		"exp":     time.Now().Add(expiry).Unix(),
		"iat":     time.Now().Unix(),
		"sub":     userID,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(testJWTSecret))
	require.NoError(t, err)
	return signed
}

func TestWSHandler_RejectsNoToken(t *testing.T) {
	hub, cancel := startHub(t)
	defer cancel()

	handler := ws.ScanProgressHandler(hub, testJWTSecret)
	scanRunID := uuid.New().String()

	req := httptest.NewRequest(http.MethodGet, "/ws/scan?scan_run_id="+scanRunID, nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestWSHandler_RejectsExpiredToken(t *testing.T) {
	hub, cancel := startHub(t)
	defer cancel()

	handler := ws.ScanProgressHandler(hub, testJWTSecret)
	scanRunID := uuid.New().String()

	expiredToken := signToken(t, uuid.New().String(), -time.Hour)

	req := httptest.NewRequest(
		http.MethodGet,
		"/ws/scan?token="+expiredToken+"&scan_run_id="+scanRunID,
		nil,
	)
	rec := httptest.NewRecorder()

	handler(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
