package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"github.com/miguel-anay/career-ops-saas/api/internal/config"
)

const googleUserInfoURL = "https://www.googleapis.com/oauth2/v3/userinfo"

// GoogleUser holds the user information returned by Google's userinfo endpoint.
type GoogleUser struct {
	ID    string `json:"sub"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// NewOAuthConfig builds the Google OAuth2 config from application config.
func NewOAuthConfig(cfg *config.Config) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     cfg.GoogleClientID,
		ClientSecret: cfg.GoogleClientSecret,
		RedirectURL:  cfg.GoogleRedirectURL,
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}
}

// ExchangeCode exchanges an OAuth2 authorization code for tokens.
func ExchangeCode(ctx context.Context, oauthCfg *oauth2.Config, code string) (*oauth2.Token, error) {
	token, err := oauthCfg.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchange oauth2 code: %w", err)
	}
	return token, nil
}

// GetUserInfo fetches the authenticated user's profile from Google.
func GetUserInfo(ctx context.Context, oauthCfg *oauth2.Config, token *oauth2.Token) (*GoogleUser, error) {
	client := oauthCfg.Client(ctx, token)

	resp, err := client.Get(googleUserInfoURL)
	if err != nil {
		return nil, fmt.Errorf("fetch google userinfo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google userinfo returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read google userinfo body: %w", err)
	}

	var user GoogleUser
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, fmt.Errorf("parse google userinfo: %w", err)
	}

	return &user, nil
}
