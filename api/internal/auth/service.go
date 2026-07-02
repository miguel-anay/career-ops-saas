package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/miguel-anay/career-ops-saas/api/internal/config"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
	"github.com/miguel-anay/career-ops-saas/api/internal/platform"
)

// PersistGmailRefreshToken upserts the Gmail incremental-consent refresh
// token onto userID's row via platform.WithTenantTx, so the write is RLS
// scoped exactly like every other tenant-table access. A second call with a
// new token REPLACES the previous one (spec scenario "re-consent replaces
// existing token") — there is no history, just the latest token.
func PersistGmailRefreshToken(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, refreshToken string) error {
	err := platform.WithTenantTx(ctx, pool, userID, func(q *db.Queries) error {
		_, err := q.UpdateUserGoogleRefreshToken(ctx, db.UpdateUserGoogleRefreshTokenParams{
			ID:                 userID,
			GoogleRefreshToken: sql.NullString{String: refreshToken, Valid: true},
		})
		return err
	})
	if err != nil {
		return fmt.Errorf("persist gmail refresh token: %w", err)
	}
	return nil
}

// ErrNotFound is returned when a user does not exist for the requesting
// tenant's RLS-scoped view (cross-tenant lookups surface this rather than
// the other tenant's row).
var ErrNotFound = errors.New("not found")

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

// GetUserByID fetches a user by UUID, scoped to callerUserID via a tenant tx
// (platform.WithTenantTx sets app.current_user_id = callerUserID for the
// duration of the query, so RLS enforces the lookup). This is the
// refresh-token read path: even though userID is trusted from a verified
// JWT, the query itself is no longer unscoped over the raw pool — a
// cross-tenant lookup (callerUserID != userID) is denied at the DB layer
// (RLS USING excludes the row, sql.ErrNoRows -> ErrNotFound) rather than
// relying on an app-layer check after an unscoped SELECT.
//
// auth.UpsertUser/auth_upsert_user are NOT touched by this — they remain
// SECURITY DEFINER and run before any tenant context exists (OAuth
// signup/login).
func GetUserByID(ctx context.Context, pool *pgxpool.Pool, callerUserID, userID uuid.UUID) (*db.User, error) {
	var u db.User

	err := platform.WithTenantTx(ctx, pool, callerUserID, func(q *db.Queries) error {
		row, qErr := q.GetUserByID(ctx, userID)
		if qErr != nil {
			return qErr
		}
		u = row
		return nil
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get user by id: %w", err)
	}

	return &u, nil
}
