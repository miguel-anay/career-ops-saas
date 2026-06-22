package tracker_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/testsupport/rlsdb"
	"github.com/miguel-anay/career-ops-saas/api/internal/tracker"
	"github.com/stretchr/testify/require"
)

// TestTrackerRLS_Integration proves that tracker.Service.UpdateApplication is
// gated by Postgres RLS USING/WITH CHECK at the DB layer, not by an app-layer
// ownership recheck — the UPDATE itself affects zero rows for a non-owner
// because the target row is invisible under RLS once the statement runs
// inside a platform.WithTenantTx scoped to the caller.
//
// Mocked Servicer tests (handler_test.go) cannot prove RLS — that is a
// database-layer invariant, exercised here against a real app_user
// connection via the shared rlsdb harness.
//
// Skips cleanly when TEST_DATABASE_URL is unset. To run against a live DB:
//
//	TEST_DATABASE_URL="postgres://app_user:app_pw@localhost:5432/careerops?sslmode=disable" \
//	  go test ./internal/tracker/... -run TestTrackerRLS_Integration -v
func TestTrackerRLS_Integration(t *testing.T) {
	ctx := context.Background()
	h := rlsdb.New(ctx, t)

	userA := h.SeedUser(ctx, t, "tracker-itest-a@test.invalid", "tracker_itest_google_a")
	userB := h.SeedUser(ctx, t, "tracker-itest-b@test.invalid", "tracker_itest_google_b")

	svc := tracker.NewService(h.AppPool)

	// Seed a jobs row + an applications row owned by user A directly via
	// AdminPool (ground truth, bypasses RLS) — applications.job_id is
	// NOT NULL UNIQUE, so the FK parent must exist first.
	jobA := mustInsertJob(ctx, t, h, userA)
	appA := mustInsertApplication(ctx, t, h, userA, jobA)

	t.Run("RLS isolation: non-owner UpdateApplication affects zero rows and is not-found", func(t *testing.T) {
		status := "Applied"
		_, err := svc.UpdateApplication(ctx, userB, appA, &status, nil)
		require.Error(t, err)
		require.ErrorIs(t, err, tracker.ErrNotFound,
			"cross-tenant UpdateApplication must surface ErrNotFound (RLS USING denial), independent of any app-layer ownership check")

		row := mustGetApplicationRow(ctx, t, h, appA)
		require.Equal(t, "Evaluated", row.Status, "A's row must be unchanged after B's denied attempt")
	})

	t.Run("owner UpdateApplication still succeeds and is visible on a subsequent read", func(t *testing.T) {
		status := "Applied"
		updated, err := svc.UpdateApplication(ctx, userA, appA, &status, nil)
		require.NoError(t, err)
		require.Equal(t, appA, updated.ID)
		require.Equal(t, userA, updated.UserID)
		require.Equal(t, "Applied", string(updated.Status))

		row := mustGetApplicationRow(ctx, t, h, appA)
		require.Equal(t, "Applied", row.Status, "A's own update must be visible on a subsequent read")
	})
}

func mustInsertJob(ctx context.Context, t *testing.T, h *rlsdb.Harness, userID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	// url carries a UUID suffix so re-running this test against a DB that
	// retains prior fixtures never collides with jobs' UNIQUE(user_id, url).
	url := "https://boards.greenhouse.io/acme/jobs/" + uuid.New().String()
	err := h.AdminPool.QueryRow(ctx,
		`INSERT INTO jobs (user_id, title, company, url) VALUES ($1, $2, $3, $4) RETURNING id`,
		userID, "Staff Engineer", "Acme Corp", url,
	).Scan(&id)
	require.NoError(t, err, "seed jobs row")
	return id
}

func mustInsertApplication(ctx context.Context, t *testing.T, h *rlsdb.Harness, userID, jobID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := h.AdminPool.QueryRow(ctx,
		`INSERT INTO applications (user_id, job_id, status) VALUES ($1, $2, 'Evaluated') RETURNING id`,
		userID, jobID,
	).Scan(&id)
	require.NoError(t, err, "seed applications row")
	return id
}

type applicationRow struct {
	ID     uuid.UUID
	UserID uuid.UUID
	Status string
}

func mustGetApplicationRow(ctx context.Context, t *testing.T, h *rlsdb.Harness, id uuid.UUID) applicationRow {
	t.Helper()
	var row applicationRow
	err := h.AdminPool.QueryRow(ctx,
		`SELECT id, user_id, status FROM applications WHERE id = $1`, id,
	).Scan(&row.ID, &row.UserID, &row.Status)
	require.NoError(t, err)
	return row
}
