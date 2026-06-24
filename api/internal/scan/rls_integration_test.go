package scan_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/scan"
	"github.com/miguel-anay/career-ops-saas/api/internal/testsupport/rlsdb"
	"github.com/stretchr/testify/require"
)

// TestScanRLS_Integration proves that scan.Service.GetScanRun is denied at
// the DB layer (RLS) in addition to the existing app-layer
// scanRun.UserID != userID check (PR #8 IDOR hotfix), and that TriggerScan
// still inserts a scan_runs row + enqueues correctly once wired through
// platform.WithTenantTx.
//
// Mocked Servicer tests (handler_test.go) cannot prove RLS — that is a
// database-layer invariant, exercised here against a real app_user
// connection via the shared rlsdb harness.
//
// Skips cleanly when TEST_DATABASE_URL is unset. To run against a live DB:
//
//	TEST_DATABASE_URL="postgres://app_user:app_pw@localhost:5432/careerops?sslmode=disable" \
//	  go test ./internal/scan/... -run TestScanRLS_Integration -v
func TestScanRLS_Integration(t *testing.T) {
	ctx := context.Background()
	h := rlsdb.New(ctx, t)
	h.EnsurePgbossSchema(ctx, t, "scan-company")

	userA := h.SeedUser(ctx, t, "scan-itest-a@test.invalid", "scan_itest_google_a")
	userB := h.SeedUser(ctx, t, "scan-itest-b@test.invalid", "scan_itest_google_b")

	svc := scan.NewService(h.AppPool)

	// Seed a scan_runs row owned by user A directly via AdminPool (ground
	// truth, bypasses RLS) so the test does not depend on TriggerScan to set
	// up the RLS-denial fixture.
	scanRunA := mustInsertScanRun(ctx, t, h, userA)

	t.Run("RLS isolation: non-owner GetScanRun is denied at the DB layer", func(t *testing.T) {
		_, err := svc.GetScanRun(ctx, userB, scanRunA)
		require.Error(t, err)
		require.True(t, errors.Is(err, sql.ErrNoRows),
			"cross-tenant GetScanRun must surface sql.ErrNoRows (RLS denial), independent of the app-layer ownership check")
	})

	t.Run("owner GetScanRun still succeeds", func(t *testing.T) {
		got, err := svc.GetScanRun(ctx, userA, scanRunA)
		require.NoError(t, err)
		require.Equal(t, scanRunA, got.ID)
		require.Equal(t, userA, got.UserID)
	})

	t.Run("TriggerScan still inserts a scan_run row and enqueues per enabled company", func(t *testing.T) {
		// Seed one enabled watched company for user A so the enqueue loop has
		// something to iterate (ground truth via AdminPool, bypasses RLS).
		mustInsertWatchedCompany(ctx, t, h, userA, "acme-corp", "https://boards.greenhouse.io/acme")

		scanRunID, err := svc.TriggerScan(ctx, userA)
		require.NoError(t, err)
		require.NotEqual(t, uuid.Nil, scanRunID)

		row := mustGetScanRunRow(ctx, t, h, scanRunID)
		require.Equal(t, userA, row.UserID)

		jobCount := countPgbossJobs(ctx, t, h, "scan-company")
		require.GreaterOrEqual(t, jobCount, 1, "TriggerScan must enqueue at least one scan-company job")
	})
}

func mustInsertScanRun(ctx context.Context, t *testing.T, h *rlsdb.Harness, userID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := h.AdminPool.QueryRow(ctx,
		`INSERT INTO scan_runs (user_id, status) VALUES ($1, 'completed') RETURNING id`,
		userID,
	).Scan(&id)
	require.NoError(t, err, "seed scan_runs row")
	return id
}

func mustInsertWatchedCompany(ctx context.Context, t *testing.T, h *rlsdb.Harness, userID uuid.UUID, name, careersURL string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := h.AdminPool.QueryRow(ctx,
		`INSERT INTO watched_companies (user_id, name, careers_url, provider_id, enabled)
		 VALUES ($1, $2, $3, 'greenhouse', true) RETURNING id`,
		userID, name, careersURL,
	).Scan(&id)
	require.NoError(t, err, "seed watched_companies row")
	return id
}

type scanRunRow struct {
	ID     uuid.UUID
	UserID uuid.UUID
}

func mustGetScanRunRow(ctx context.Context, t *testing.T, h *rlsdb.Harness, id uuid.UUID) scanRunRow {
	t.Helper()
	var row scanRunRow
	err := h.AdminPool.QueryRow(ctx, `SELECT id, user_id FROM scan_runs WHERE id = $1`, id).Scan(&row.ID, &row.UserID)
	require.NoError(t, err)
	return row
}

func countPgbossJobs(ctx context.Context, t *testing.T, h *rlsdb.Harness, name string) int {
	t.Helper()
	var count int
	err := h.AdminPool.QueryRow(ctx, `SELECT count(*) FROM pgboss.job WHERE name = $1`, name).Scan(&count)
	require.NoError(t, err)
	return count
}
