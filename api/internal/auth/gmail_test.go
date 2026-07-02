package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// package auth (internal test) — not auth_test — because it needs to
// override the unexported exchangeGmailCode/persistGmailToken seams on
// Handler to avoid hitting real Google/DB network in these unit tests
// (mirrors how PersistGmailRefreshToken itself is proven separately, DB-gated,
// in service_test.go).
//
// HandleGmailOAuth checks the Bearer token itself (auth.VerifyToken) rather
// than depending on middleware.Authenticator — the middleware package
// imports auth for VerifyToken, so auth importing middleware back would be a
// cycle. This keeps /auth/google/gmail self-contained within the auth domain.

const gmailTestJWTSecret = "gmail-test-jwt-secret"

func newTestConfig() *config.Config {
	return &config.Config{
		JWTSecret:          gmailTestJWTSecret,
		GoogleClientID:     "test-client-id",
		GoogleClientSecret: "test-client-secret",
		GoogleRedirectURL:  "http://localhost:8080/auth/google/callback",
		WebOrigin:          "http://localhost:3000",
	}
}

// ---- HandleGmailOAuth (GET /auth/google/gmail) ----

func TestHandleGmailOAuth_RedirectIncludesScopeAndConsentParams(t *testing.T) {
	cfg := newTestConfig()
	h := NewHandler(cfg, nil)
	userID := uuid.New()
	accessToken, err := IssueAccessToken(userID.String(), "free", cfg.JWTSecret)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/auth/google/gmail", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	rec := httptest.NewRecorder()

	h.HandleGmailOAuth(rec, req)

	require.Equal(t, http.StatusTemporaryRedirect, rec.Code)

	loc := rec.Header().Get("Location")
	require.NotEmpty(t, loc)
	parsed, err := url.Parse(loc)
	require.NoError(t, err)

	q := parsed.Query()
	assert.Contains(t, q.Get("scope"), "gmail.readonly", "redirect URL scope must include gmail.readonly")
	assert.Equal(t, "consent", q.Get("prompt"), "redirect URL must force prompt=consent so an already-linked account still issues a refresh_token")
	assert.Equal(t, "offline", q.Get("access_type"), "redirect URL must request access_type=offline")
	assert.NotEmpty(t, q.Get("state"))
}

func TestHandleGmailOAuth_MissingAuth(t *testing.T) {
	h := NewHandler(newTestConfig(), nil)

	req := httptest.NewRequest(http.MethodGet, "/auth/google/gmail", nil)
	rec := httptest.NewRecorder()

	h.HandleGmailOAuth(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// ---- HandleGmailOAuthCallback (GET /auth/google/gmail/callback) ----

func TestHandleGmailOAuthCallback_InvalidState(t *testing.T) {
	h := NewHandler(newTestConfig(), nil)

	req := httptest.NewRequest(http.MethodGet, "/auth/google/gmail/callback?state=bogus&code=authcode", nil)
	rec := httptest.NewRecorder()

	h.HandleGmailOAuthCallback(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleGmailOAuthCallback_MismatchedCookie(t *testing.T) {
	h := NewHandler(newTestConfig(), nil)
	userID := uuid.New()
	state := mustSignGmailState(t, userID.String(), gmailTestJWTSecret)

	req := httptest.NewRequest(http.MethodGet, "/auth/google/gmail/callback?state="+state+"&code=authcode", nil)
	req.AddCookie(&http.Cookie{Name: "gmail_oauth_state", Value: "different-value"})
	rec := httptest.NewRecorder()

	h.HandleGmailOAuthCallback(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestHandleGmailOAuthCallback_Success proves the callback validates state,
// calls PersistGmailRefreshToken (via the injectable seam) with the userID
// recovered from state, and does NOT issue app JWTs (no access_token in the
// response, no refresh_token cookie set) — spec scenario "first-time Gmail
// connection".
func TestHandleGmailOAuthCallback_Success(t *testing.T) {
	h := NewHandler(newTestConfig(), nil)
	userID := uuid.New()
	state := mustSignGmailState(t, userID.String(), gmailTestJWTSecret)

	h.exchangeGmailCode = func(ctx context.Context, code string) (*oauth2.Token, error) {
		assert.Equal(t, "authcode", code)
		return &oauth2.Token{RefreshToken: "gmail-refresh-token"}, nil
	}

	var persistedUserID uuid.UUID
	var persistedToken string
	h.persistGmailToken = func(ctx context.Context, uid uuid.UUID, refreshToken string) error {
		persistedUserID = uid
		persistedToken = refreshToken
		return nil
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/google/gmail/callback?state="+state+"&code=authcode", nil)
	req.AddCookie(&http.Cookie{Name: "gmail_oauth_state", Value: state})
	rec := httptest.NewRecorder()

	h.HandleGmailOAuthCallback(rec, req)

	require.True(t, rec.Code >= 300 && rec.Code < 400, "callback must redirect back to web, got %d", rec.Code)

	assert.Equal(t, userID, persistedUserID, "PersistGmailRefreshToken must be called with the userID recovered from state")
	assert.Equal(t, "gmail-refresh-token", persistedToken)

	// No app JWTs: no refresh_token cookie, no access_token anywhere in the response.
	for _, c := range rec.Result().Cookies() {
		assert.NotEqual(t, "refresh_token", c.Name, "gmail callback must not set the app refresh_token cookie")
	}
	assert.False(t, strings.Contains(rec.Header().Get("Location"), "access_token"),
		"gmail callback must not issue an app access_token")
}

func TestHandleGmailOAuthCallback_MissingCode(t *testing.T) {
	h := NewHandler(newTestConfig(), nil)
	userID := uuid.New()
	state := mustSignGmailState(t, userID.String(), gmailTestJWTSecret)

	req := httptest.NewRequest(http.MethodGet, "/auth/google/gmail/callback?state="+state, nil)
	req.AddCookie(&http.Cookie{Name: "gmail_oauth_state", Value: state})
	rec := httptest.NewRecorder()

	h.HandleGmailOAuthCallback(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ---- NewGmailOAuthConfig (pure) ----

func TestNewGmailOAuthConfig_ScopesAndRedirect(t *testing.T) {
	cfg := newTestConfig()
	oauthCfg := NewGmailOAuthConfig(cfg)

	assert.Contains(t, oauthCfg.Scopes, "https://www.googleapis.com/auth/gmail.readonly")
	assert.Contains(t, oauthCfg.Scopes, "openid")
	assert.Contains(t, oauthCfg.Scopes, "email")
	assert.Contains(t, oauthCfg.Scopes, "profile")
	assert.Equal(t, "http://localhost:8080/auth/google/gmail/callback", oauthCfg.RedirectURL)
}

// ---- test helpers ----

func mustSignGmailState(t *testing.T, userID, secret string) string {
	t.Helper()
	state, err := signGmailState(userID, secret)
	require.NoError(t, err)
	return state
}

// sanity check that state signing round-trips and rejects tampering /
// wrong-algorithm tokens, independent of the HTTP handler tests above.
func TestVerifyGmailState_RejectsTamperedToken(t *testing.T) {
	userID := uuid.New()
	state, err := signGmailState(userID.String(), gmailTestJWTSecret)
	require.NoError(t, err)

	_, err = verifyGmailState(state, "wrong-secret")
	assert.Error(t, err)

	// alg confusion guard
	unsigned := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.RegisteredClaims{Subject: userID.String()})
	unsignedStr, err := unsigned.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)
	_, err = verifyGmailState(unsignedStr, gmailTestJWTSecret)
	assert.Error(t, err)
}
