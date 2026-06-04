package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/miguel-anay/career-ops-saas/api/internal/config"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
)

// UpsertUser calls the SECURITY DEFINER auth_upsert_user function to create or
// update the user record. This bypasses RLS because the tenant user_id is not
// yet known at OAuth callback time.
func UpsertUser(ctx context.Context, pool *pgxpool.Pool, googleUser *GoogleUser) (*db.User, error) {
	const query = `SELECT * FROM auth_upsert_user($1, $2, $3)`

	row := pool.QueryRow(ctx, query, googleUser.Email, googleUser.ID, googleUser.Name)

	var u db.User
	var cvMarkdown *string
	var profileJSON []byte
	var receivedAt *time.Time

	err := row.Scan(
		&u.ID,
		&u.Email,
		&u.GoogleID,
		&u.Plan,
		&cvMarkdown,
		&profileJSON,
		&u.CreatedAt,
	)
	_ = receivedAt

	if err != nil {
		return nil, fmt.Errorf("upsert user: %w", err)
	}

	if cvMarkdown != nil {
		u.CvMarkdown.String = *cvMarkdown
		u.CvMarkdown.Valid = true
	}
	if len(profileJSON) > 0 {
		u.ProfileJson = profileJSON
	}

	return &u, nil
}

// IssueTokenPair issues both an access token and a refresh token for the given user.
func IssueTokenPair(user *db.User, cfg *config.Config) (accessToken, refreshToken string, err error) {
	userIDStr := user.ID.String()
	planStr := string(user.Plan)

	accessToken, err = IssueAccessToken(userIDStr, planStr, cfg.JWTSecret)
	if err != nil {
		return "", "", fmt.Errorf("issue access token: %w", err)
	}

	refreshToken, err = IssueRefreshToken(userIDStr, cfg.JWTRefreshSecret)
	if err != nil {
		return "", "", fmt.Errorf("issue refresh token: %w", err)
	}

	return accessToken, refreshToken, nil
}

// GetUserByID fetches a user by UUID from the pool without tenant scoping.
// Used internally (e.g. refresh token flow) where the user_id is trusted from the JWT.
func GetUserByID(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) (*db.User, error) {
	const query = `SELECT id, email, google_id, plan, cv_markdown, profile_json, created_at FROM users WHERE id = $1 LIMIT 1`

	row := pool.QueryRow(ctx, query, userID)

	var u db.User
	var cvMarkdown *string
	var profileJSON []byte

	err := row.Scan(
		&u.ID,
		&u.Email,
		&u.GoogleID,
		&u.Plan,
		&cvMarkdown,
		&profileJSON,
		&u.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}

	if cvMarkdown != nil {
		u.CvMarkdown.String = *cvMarkdown
		u.CvMarkdown.Valid = true
	}
	if len(profileJSON) > 0 {
		u.ProfileJson = profileJSON
	}

	return &u, nil
}
