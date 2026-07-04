package auth_test

import (
	"context"
	"testing"

	"github.com/miguel-anay/career-ops-saas/api/internal/auth"
	"github.com/miguel-anay/career-ops-saas/api/internal/testsupport/rlsdb"
	"github.com/stretchr/testify/require"
)

// TestPersistGmailRefreshToken proves the upsert semantics required by the
// spec scenarios "First-time Gmail connection" and "Re-consent replaces
// existing token": the first call stores a refresh token on a user with
// none, and a second call replaces the previously stored token rather than
// erroring or appending.
//
// DB-gated: skips cleanly when TEST_DATABASE_URL is unset (see rlsdb.New).
func TestPersistGmailRefreshToken(t *testing.T) {
	ctx := context.Background()
	h := rlsdb.New(ctx, t)

	userID := h.SeedUser(ctx, t, "gmail-persist-itest@test.invalid", "gmail_persist_itest_google")

	t.Run("first-time connection stores the refresh token", func(t *testing.T) {
		err := auth.PersistGmailRefreshToken(ctx, h.AppPool, userID, "refresh-token-v1")
		require.NoError(t, err)

		u, err := auth.GetUserByID(ctx, h.AppPool, userID, userID)
		require.NoError(t, err)
		require.True(t, u.GoogleRefreshToken.Valid)
		require.Equal(t, "refresh-token-v1", u.GoogleRefreshToken.String)
	})

	t.Run("re-consent replaces the existing token", func(t *testing.T) {
		err := auth.PersistGmailRefreshToken(ctx, h.AppPool, userID, "refresh-token-v2")
		require.NoError(t, err)

		u, err := auth.GetUserByID(ctx, h.AppPool, userID, userID)
		require.NoError(t, err)
		require.True(t, u.GoogleRefreshToken.Valid)
		require.Equal(t, "refresh-token-v2", u.GoogleRefreshToken.String)
	})
}

// TestUpsertUser proves the OAuth-callback upsert survives schema growth:
// auth_upsert_user RETURNS users, so any migration that adds a column to
// users changes the result arity. A `SELECT *` with a positional scan breaks
// login for every user the moment such a migration lands (regression: 006
// added google_refresh_token and the callback started returning 500).
//
// DB-gated: skips cleanly when TEST_DATABASE_URL is unset (see rlsdb.New).
func TestUpsertUser(t *testing.T) {
	ctx := context.Background()
	h := rlsdb.New(ctx, t)

	gu := &auth.GoogleUser{
		ID:    "upsert-itest-google",
		Email: "upsert-itest@test.invalid",
		Name:  "Upsert Itest",
	}

	u, err := auth.UpsertUser(ctx, h.AppPool, gu)
	require.NoError(t, err)
	require.Equal(t, gu.Email, u.Email)

	again, err := auth.UpsertUser(ctx, h.AppPool, gu)
	require.NoError(t, err)
	require.Equal(t, u.ID, again.ID)
}
