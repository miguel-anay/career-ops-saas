package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/miguel-anay/career-ops-saas/api/internal/config"
	"golang.org/x/oauth2"
)

// Handler holds the dependencies for auth HTTP handlers.
type Handler struct {
	cfg      *config.Config
	oauthCfg *oauth2.Config
	pool     *pgxpool.Pool
}

// NewHandler creates a new auth Handler.
func NewHandler(cfg *config.Config, pool *pgxpool.Pool) *Handler {
	return &Handler{
		cfg:      cfg,
		oauthCfg: NewOAuthConfig(cfg),
		pool:     pool,
	}
}

// GoogleLogin redirects the user to Google's OAuth2 consent screen.
// GET /auth/google
func (h *Handler) GoogleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := generateState()
	if err != nil {
		http.Error(w, `{"error":"failed to generate state"}`, http.StatusInternalServerError)
		return
	}

	// Store state in a short-lived cookie for CSRF protection.
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   300,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	url := h.oauthCfg.AuthCodeURL(state, oauth2.AccessTypeOffline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// GoogleCallback handles the OAuth2 callback from Google.
// GET /auth/google/callback
func (h *Handler) GoogleCallback(w http.ResponseWriter, r *http.Request) {
	// Validate state parameter.
	stateCookie, err := r.Cookie("oauth_state")
	if err != nil || r.URL.Query().Get("state") != stateCookie.Value {
		http.Error(w, `{"error":"invalid oauth state"}`, http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, `{"error":"missing authorization code"}`, http.StatusBadRequest)
		return
	}

	token, err := ExchangeCode(r.Context(), h.oauthCfg, code)
	if err != nil {
		http.Error(w, `{"error":"failed to exchange code"}`, http.StatusInternalServerError)
		return
	}

	googleUser, err := GetUserInfo(r.Context(), h.oauthCfg, token)
	if err != nil {
		http.Error(w, `{"error":"failed to get user info"}`, http.StatusInternalServerError)
		return
	}

	user, err := UpsertUser(r.Context(), h.pool, googleUser)
	if err != nil {
		http.Error(w, `{"error":"failed to upsert user"}`, http.StatusInternalServerError)
		return
	}

	accessToken, refreshToken, err := IssueTokenPair(user, h.cfg)
	if err != nil {
		http.Error(w, `{"error":"failed to issue tokens"}`, http.StatusInternalServerError)
		return
	}

	// Set refresh token as httpOnly cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    refreshToken,
		Path:     "/auth",
		MaxAge:   7 * 24 * 3600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	// Redirect to frontend with access token in query param.
	redirectURL := fmt.Sprintf("%s/auth/callback?access_token=%s", h.cfg.WebOrigin, accessToken)
	http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
}

// Refresh validates the refresh token and issues a new token pair (rotation).
// POST /auth/refresh
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	// Read refresh token from cookie or request body.
	refreshToken := ""
	if cookie, err := r.Cookie("refresh_token"); err == nil {
		refreshToken = cookie.Value
	} else {
		var body struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			refreshToken = body.RefreshToken
		}
	}

	if refreshToken == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing refresh token"})
		return
	}

	claims, err := VerifyToken(refreshToken, h.cfg.JWTRefreshSecret)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired refresh token"})
		return
	}

	// Issue new token pair (rotation).
	newAccess, err := IssueAccessToken(claims.UserID, claims.Plan, h.cfg.JWTSecret)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue access token"})
		return
	}

	newRefresh, err := IssueRefreshToken(claims.UserID, h.cfg.JWTRefreshSecret)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue refresh token"})
		return
	}

	// Rotate the refresh token cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    newRefresh,
		Path:     "/auth",
		MaxAge:   7 * 24 * 3600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	writeJSON(w, http.StatusOK, map[string]string{"access_token": newAccess})
}

// Logout clears the refresh token cookie. For MVP: client-side only.
// POST /auth/logout
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     "/auth",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
