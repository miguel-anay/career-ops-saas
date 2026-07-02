package auth

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/miguel-anay/career-ops-saas/api/internal/config"
)

// gmailReadonlyScope is the single Gmail scope this feature is allowed to
// request — read-only, per the spec's "Read-Only Privacy Constraint". Never
// add gmail.modify or gmail.labels here.
const gmailReadonlyScope = "https://www.googleapis.com/auth/gmail.readonly"

// gmailStateCookie is the CSRF cookie name for the incremental-consent flow.
// Kept distinct from the login flow's "oauth_state" cookie so the two flows
// never collide if both are mid-flight in the same browser.
const gmailStateCookie = "gmail_oauth_state"

// gmailStateTTL is deliberately short — the state token only needs to
// survive the redirect round-trip to Google and back.
const gmailStateTTL = 10 * time.Minute

// NewGmailOAuthConfig builds a second, separate oauth2.Config for the
// gmail.readonly incremental-consent flow. Kept apart from NewOAuthConfig
// (login) so login's scope never grows: an existing user's session is never
// forced to re-consent just because this feature exists (spec: "Existing
// users MUST NOT be forced to re-authenticate").
func NewGmailOAuthConfig(cfg *config.Config) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     cfg.GoogleClientID,
		ClientSecret: cfg.GoogleClientSecret,
		RedirectURL:  gmailRedirectURL(cfg.GoogleRedirectURL),
		Scopes:       []string{"openid", "email", "profile", gmailReadonlyScope},
		Endpoint:     google.Endpoint,
	}
}

// gmailRedirectURL derives the incremental-consent callback URL from the
// login callback URL already configured via GOOGLE_REDIRECT_URL, so no new
// required env var is introduced for this feature.
// ponytail: assumes GOOGLE_REDIRECT_URL ends in "/auth/google/callback" (true
// for every deployment target registered in main.go); falls back to
// appending "/gmail/callback" if that suffix isn't found.
func gmailRedirectURL(base string) string {
	const loginSuffix = "/auth/google/callback"
	if strings.HasSuffix(base, loginSuffix) {
		return strings.TrimSuffix(base, loginSuffix) + "/auth/google/gmail/callback"
	}
	return strings.TrimSuffix(base, "/") + "/gmail/callback"
}

// HandleGmailOAuth returns the Google incremental-consent URL for
// gmail.readonly as JSON, `{"auth_url": "..."}`. GET /auth/google/gmail.
//
// This is Bearer-authenticated (see bearerUserID below), so it must be
// called through an authenticated client — web/lib/api.ts's apiGet — rather
// than a plain top-level browser navigation, which carries no Authorization
// header and would always 401. The caller reads auth_url from the response
// and THEN does the actual browser navigation to Google.
//
// Authentication is checked in-package via VerifyToken (Bearer header)
// rather than middleware.Authenticator: the middleware package imports auth
// for VerifyToken, so auth importing middleware back would be an import
// cycle.
func (h *Handler) HandleGmailOAuth(w http.ResponseWriter, r *http.Request) {
	userID, ok := bearerUserID(r, h.cfg.JWTSecret)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	state, err := signGmailState(userID.String(), h.cfg.JWTSecret)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate state"})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     gmailStateCookie,
		Value:    state,
		Path:     "/",
		MaxAge:   int(gmailStateTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	url := h.gmailOAuthCfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
	)
	writeJSON(w, http.StatusOK, map[string]string{"auth_url": url})
}

// HandleGmailOAuthCallback validates state, exchanges the code, persists the
// refresh token, and redirects back to web. It does NOT issue app JWTs — the
// user is already logged in; this flow only grants an additional scope.
// GET /auth/google/gmail/callback.
func (h *Handler) HandleGmailOAuthCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	stateCookie, err := r.Cookie(gmailStateCookie)
	if state == "" || err != nil || state != stateCookie.Value {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid oauth state"})
		return
	}

	userID, err := verifyGmailState(state, h.cfg.JWTSecret)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid oauth state"})
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing authorization code"})
		return
	}

	token, err := h.exchangeGmailCode(r.Context(), code)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to exchange code"})
		return
	}
	if token.RefreshToken == "" {
		// Google only issues a refresh_token on first authorization or when
		// prompt=consent forces it — HandleGmailOAuth always sets
		// prompt=consent, so this should not happen in practice, but fail
		// loudly rather than persisting an empty token (Risk 2).
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "no refresh token returned"})
		return
	}

	if err := h.persistGmailToken(r.Context(), userID, token.RefreshToken); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to persist gmail token"})
		return
	}

	http.Redirect(w, r, h.cfg.WebOrigin+"/settings?gmail=connected", http.StatusTemporaryRedirect)
}

// bearerUserID extracts and verifies a Bearer JWT from the Authorization
// header, returning the parsed userID. Mirrors middleware.Authenticator's
// checks without importing that package (see cycle note above).
func bearerUserID(r *http.Request, jwtSecret string) (uuid.UUID, bool) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		return uuid.Nil, false
	}
	claims, err := VerifyToken(strings.TrimPrefix(authHeader, "Bearer "), jwtSecret)
	if err != nil {
		return uuid.Nil, false
	}
	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		return uuid.Nil, false
	}
	return userID, true
}

// gmailStateTyp is a dedicated claim value that makes the gmail-state JWT
// non-interchangeable with access/refresh tokens. Both share the same
// JWTSecret and both carry a Subject/UserID, so a bare jwt.RegisteredClaims
// state token would accept ANY valid access or refresh token as state — an
// attacker holding a victim's leaked access token could pass it as state and
// attach their own Gmail refresh token to the victim's account. Requiring
// this claim (present here, absent from Claims in jwt.go) closes that hole:
// verifyGmailState rejects any token that doesn't carry it.
const gmailStateTyp = "gmail_state"

// gmailStateClaims is the claims shape for signGmailState/verifyGmailState —
// deliberately distinct from auth.Claims (access/refresh tokens) so the two
// token families can never be swapped for one another.
type gmailStateClaims struct {
	Typ string `json:"typ"`
	jwt.RegisteredClaims
}

// signGmailState signs a short-lived, single-purpose JWT carrying userID as
// its subject and the gmail_state typ claim. It reuses the app's JWT secret
// (no new secret/env var) but is a distinct claims shape/TTL from
// access/refresh tokens — it is only ever verified by verifyGmailState,
// never accepted by the Authenticator middleware (different claims struct).
func signGmailState(userID, secret string) (string, error) {
	claims := gmailStateClaims{
		Typ: gmailStateTyp,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(gmailStateTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("sign gmail state: %w", err)
	}
	return signed, nil
}

// verifyGmailState validates a state token produced by signGmailState and
// returns the userID it carries. Rejects any token missing the gmail_state
// typ claim — including otherwise-valid access/refresh tokens signed with
// the same secret (see gmailStateTyp doc comment).
func verifyGmailState(state, secret string) (uuid.UUID, error) {
	token, err := jwt.ParseWithClaims(state, &gmailStateClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse gmail state: %w", err)
	}
	claims, ok := token.Claims.(*gmailStateClaims)
	if !ok || !token.Valid {
		return uuid.Nil, fmt.Errorf("invalid gmail state claims")
	}
	if claims.Typ != gmailStateTyp {
		return uuid.Nil, fmt.Errorf("wrong token type for gmail state")
	}
	return uuid.Parse(claims.Subject)
}
