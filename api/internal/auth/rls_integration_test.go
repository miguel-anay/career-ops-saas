package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/miguel-anay/career-ops-saas/api/internal/auth"
	"github.com/miguel-anay/career-ops-saas/api/internal/testsupport/rlsdb"
	"github.com/stretchr/testify/require"
)

// TestAuthRLS_Integration proves that auth.GetUserByID is gated by Postgres
// RLS at the DB layer for the refresh-token read path: a request scoped to
// user B (via a tenant tx set to B's user_id) cannot read user A's row, even
// though GetUserByID takes the target userID as a plain argument with no
// app-layer "owner == target" check of its own.
//
// Mocked tests cannot prove this — it is a database-layer invariant,
// exercised here against a real app_user connection via the shared rlsdb
// harness.
//
// Skips cleanly when TEST_DATABASE_URL is unset. To run against a live DB:
//
//	TEST_DATABASE_URL="postgres://app_user:app_pw@localhost:5432/careerops?sslmode=disable" \
//	  go test ./internal/auth/... -run TestAuthRLS_Integration -v
func TestAuthRLS_Integration(t *testing.T) {
	ctx := context.Background()
	h := rlsdb.New(ctx, t)

	userA := h.SeedUser(ctx, t, "auth-itest-a@test.invalid", "auth_itest_google_a")
	userB := h.SeedUser(ctx, t, "auth-itest-b@test.invalid", "auth_itest_google_b")

	t.Run("RLS isolation: cross-tenant GetUserByID returns not-found", func(t *testing.T) {
		// userB's tenant tx is asked to look up userA's row — RLS USING
		// excludes it, so the query returns no row.
		u, err := auth.GetUserByID(ctx, h.AppPool, userB, userA)
		require.Error(t, err)
		require.Nil(t, u)
		require.True(t, errors.Is(err, auth.ErrNotFound),
			"cross-tenant GetUserByID must surface auth.ErrNotFound (RLS USING denial), not userA's row")
	})

	t.Run("owner GetUserByID still succeeds and returns the caller's own row", func(t *testing.T) {
		u, err := auth.GetUserByID(ctx, h.AppPool, userA, userA)
		require.NoError(t, err)
		require.NotNil(t, u)
		require.Equal(t, userA, u.ID)
	})
}
