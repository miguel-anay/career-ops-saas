package jobs_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/jobs"
	"github.com/miguel-anay/career-ops-saas/api/internal/testsupport/rlsdb"
	"github.com/stretchr/testify/require"
)

// TestJobsRLS_Integration proves that jobs.Service.GetByID/List/AddManual are
// gated by Postgres RLS at the DB layer — a non-owner's GetByID call returns
// zero rows from GetJobByID itself (RLS USING denial), not merely an
// app-layer ownership recheck running after an unscoped query.
//
// Mocked Servicer tests (handler_test.go) cannot prove RLS — that is a
// database-layer invariant, exercised here against a real app_user
// connection via the shared rlsdb harness.
//
// Skips cleanly when TEST_DATABASE_URL is unset. To run against a live DB:
//
//	TEST_DATABASE_URL="postgres://app_user:app_pw@localhost:5432/careerops?sslmode=disable" \
//	  go test ./internal/jobs/... -run TestJobsRLS_Integration -v
func TestJobsRLS_Integration(t *testing.T) {
	ctx := context.Background()
	h := rlsdb.New(ctx, t)

	userA := h.SeedUser(ctx, t, "jobs-itest-a@test.invalid", "jobs_itest_google_a")
	userB := h.SeedUser(ctx, t, "jobs-itest-b@test.invalid", "jobs_itest_google_b")

	// jobs has no unique constraint that lets re-runs collide-and-replace
	// (unlike e.g. an upsert keyed on (user_id, url) alone reused verbatim);
	// each run adds a new uuid-suffixed URL, so a stable seeded user's job
	// list grows across repeated runs against a persistent DB. Clear any
	// prior fixtures for these two test users before asserting List's count.
	_, err := h.AdminPool.Exec(ctx, `DELETE FROM jobs WHERE user_id = ANY($1)`, []uuid.UUID{userA, userB})
	require.NoError(t, err, "clear stale job fixtures")

	svc := jobs.NewService(h.AppPool)

	jobA, err := svc.AddManual(ctx, userA, "https://boards.greenhouse.io/acme/jobs/"+uuid.New().String())
	require.NoError(t, err, "owner AddManual must succeed")
	require.Equal(t, userA, jobA.UserID)

	t.Run("RLS isolation: non-owner GetByID returns zero rows / not-found", func(t *testing.T) {
		_, err := svc.GetByID(ctx, userB, jobA.ID)
		require.Error(t, err)
		require.ErrorIs(t, err, jobs.ErrNotFound,
			"cross-tenant GetByID must surface ErrNotFound (RLS USING denial)")
	})

	t.Run("RLS isolation: non-owner List never returns A's jobs", func(t *testing.T) {
		jobsForB, err := svc.List(ctx, userB, 1, 20)
		require.NoError(t, err)
		for _, j := range jobsForB {
			require.NotEqual(t, jobA.ID, j.ID, "B's List must never include A's job")
			require.Equal(t, userB, j.UserID)
		}
	})

	t.Run("owner GetByID/List/AddManual still succeed", func(t *testing.T) {
		got, err := svc.GetByID(ctx, userA, jobA.ID)
		require.NoError(t, err)
		require.Equal(t, jobA.ID, got.ID)
		require.Equal(t, userA, got.UserID)

		jobsForA, err := svc.List(ctx, userA, 1, 20)
		require.NoError(t, err)
		require.Len(t, jobsForA, 1, "A's List must return exactly A's own job")
		require.Equal(t, jobA.ID, jobsForA[0].ID)

		jobA2, err := svc.AddManual(ctx, userA, "https://jobs.lever.co/acme/"+uuid.New().String())
		require.NoError(t, err)
		require.Equal(t, userA, jobA2.UserID)
	})
}
