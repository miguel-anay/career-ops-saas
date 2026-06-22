package cv_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/miguel-anay/career-ops-saas/api/internal/cv"
	"github.com/miguel-anay/career-ops-saas/api/internal/platform"
	"github.com/miguel-anay/career-ops-saas/api/internal/testsupport/rlsdb"
	"github.com/stretchr/testify/require"
)

// TestCVIngest_RLS_Integration proves that EnqueueIngest and GetIngestion
// actually engage RLS against a real, non-superuser app_user connection.
// Mocked Servicer tests (handler_test.go) cannot prove this — RLS is a
// database-layer invariant.
//
// Skips cleanly when TEST_DATABASE_URL is unset. To run against a live DB:
//
//	TEST_DATABASE_URL="postgres://app_user:app_pw@localhost:5432/careerops?sslmode=disable" \
//	  go test ./internal/cv/... -run TestCVIngest_RLS_Integration -v
//
// Two connections are used deliberately:
//   - appPool   (app_user, RLS-enforced) exercises the Service so the RLS
//     assertions are truthful — a superuser would bypass RLS and make them
//     false positives.
//   - adminPool (table owner / superuser) bootstraps the pg-boss stand-in
//     schema and inspects ground truth (usage/cv_ingestions) WITHOUT RLS,
//     because reading those rows as app_user would require a tenant context.
//
// The admin DSN defaults to the same host with the careerops superuser creds
// (matching docker-compose.yml); override with TEST_ADMIN_DATABASE_URL.
//
// The target DB must have migrations 001_initial.sql + 002_ingest_cv.sql
// applied, connecting as the app_user role for TEST_DATABASE_URL.
func TestCVIngest_RLS_Integration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run cv ingest integration tests")
	}
	adminDSN := os.Getenv("TEST_ADMIN_DATABASE_URL")
	if adminDSN == "" {
		// Derive the privileged DSN from the app DSN by swapping credentials.
		adminDSN = strings.Replace(dsn, "app_user:app_pw", "careerops:careerops", 1)
	}

	ctx := context.Background()

	appPool, err := platform.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer appPool.Close()

	adminPool, err := platform.NewPool(ctx, adminDSN)
	require.NoError(t, err, "admin connection (set TEST_ADMIN_DATABASE_URL for a superuser/owner DSN)")
	defer adminPool.Close()

	// pg-boss creates its schema at worker boot; a bare migrated DB has none.
	// Create a minimal stand-in sufficient for queue.Enqueue's INSERT so the
	// API enqueue path runs end-to-end. This is a test fixture, not the real
	// pg-boss schema.
	ensurePgbossStandin(ctx, t, adminPool)

	svc := cv.NewService(appPool)

	// Seed two independent users via the auth_upsert_user SECURITY DEFINER
	// helper (bypasses RLS for setup, exactly like production OAuth signup —
	// see db/tests/cv_ingestions_rls.test.sql for the same pattern).
	userA := mustUpsertUser(ctx, t, adminPool, "ingest-itest-a@test.invalid", "ingest_itest_google_a")
	userB := mustUpsertUser(ctx, t, adminPool, "ingest-itest-b@test.invalid", "ingest_itest_google_b")

	// Clean up any stale usage rows for this month so the limit-gating
	// assertions below start from a known state.
	month := time.Now().UTC().Format("2006-01")
	cleanupUsage(ctx, t, adminPool, userA, month)
	cleanupUsage(ctx, t, adminPool, userB, month)

	t.Run("owner enqueue increments usage and creates a row", func(t *testing.T) {
		runID, err := svc.EnqueueIngest(ctx, userA, "raw cv text for user A")
		require.NoError(t, err)
		require.NotEqual(t, uuid.Nil, runID)

		assertIngestionsCount(ctx, t, adminPool, userA, month, 1)
		assertEvaluationsCount(ctx, t, adminPool, userA, month, 0)

		row := mustGetIngestionRow(ctx, t, adminPool, runID)
		require.Equal(t, userA, row.UserID)
	})

	t.Run("second enqueue increments counter independently of evaluations_count", func(t *testing.T) {
		// Pre-seed evaluations_count = 3 to prove ingestions_count moves
		// independently (Req 6: distinct counters per Decision 6).
		seedEvaluationsCount(ctx, t, adminPool, userA, month, 3)

		_, err := svc.EnqueueIngest(ctx, userA, "second raw cv text for user A")
		require.NoError(t, err)

		assertIngestionsCount(ctx, t, adminPool, userA, month, 2)
		assertEvaluationsCount(ctx, t, adminPool, userA, month, 3)
	})

	t.Run("first ingestion of the month with no usage row succeeds and counts as zero baseline", func(t *testing.T) {
		cleanupUsage(ctx, t, adminPool, userB, month)

		runID, err := svc.EnqueueIngest(ctx, userB, "raw cv text for user B")
		require.NoError(t, err)
		require.NotEqual(t, uuid.Nil, runID)

		assertIngestionsCount(ctx, t, adminPool, userB, month, 1)
	})

	t.Run("limit gating blocks the 6th enqueue and does not increment usage", func(t *testing.T) {
		cleanupUsage(ctx, t, adminPool, userB, month)
		countIngestionsBefore := countCVIngestions(ctx, t, adminPool, userB)

		// Drive ingestions_count to the limit (5) via successful enqueues.
		for i := 0; i < 5; i++ {
			_, err := svc.EnqueueIngest(ctx, userB, fmt.Sprintf("raw cv #%d", i))
			require.NoError(t, err)
		}
		assertIngestionsCount(ctx, t, adminPool, userB, month, 5)
		countIngestionsAtLimit := countCVIngestions(ctx, t, adminPool, userB)
		require.Equal(t, countIngestionsBefore+5, countIngestionsAtLimit)

		// The 6th call must be rejected with ErrUsageLimitExceeded.
		_, err := svc.EnqueueIngest(ctx, userB, "raw cv that should be rejected")
		require.ErrorIs(t, err, cv.ErrUsageLimitExceeded)

		// No new row, no further increment.
		assertIngestionsCount(ctx, t, adminPool, userB, month, 5)
		countIngestionsAfterRejection := countCVIngestions(ctx, t, adminPool, userB)
		require.Equal(t, countIngestionsAtLimit, countIngestionsAfterRejection,
			"rejected enqueue must not create a cv_ingestions row")
	})

	t.Run("RLS isolation: owner can read, non-owner gets ErrNotFound", func(t *testing.T) {
		runID, err := svc.EnqueueIngest(ctx, userA, "raw cv for RLS isolation check")
		require.NoError(t, err)

		got, err := svc.GetIngestion(ctx, userA, runID)
		require.NoError(t, err)
		require.Equal(t, runID, got.ID)
		require.Equal(t, userA, got.UserID)

		_, err = svc.GetIngestion(ctx, userB, runID)
		require.ErrorIs(t, err, cv.ErrNotFound,
			"non-owner lookup must be denied by RLS (sql.ErrNoRows -> ErrNotFound), not an app-layer check")
	})
}

// ensurePgbossStandin creates a minimal pgboss.job table matching the columns
// queue.Enqueue inserts, and grants app_user INSERT, so the enqueue path runs
// against a bare migrated DB. The real schema is created by the pg-boss runtime.
//
// Delegates to rlsdb.EnsurePgbossStandin (the same DDL, behind a
// pg_advisory_xact_lock) rather than running this duplicate inline copy
// directly. This file predates the shared testsupport/rlsdb harness
// (it's from the ingest-cv change); a second, lock-free copy of the same
// "CREATE SCHEMA/TABLE IF NOT EXISTS" DDL racing against the harness's
// locked copy from other packages caused intermittent
// "tuple concurrently updated" failures when test packages ran in parallel.
func ensurePgbossStandin(ctx context.Context, t *testing.T, admin *pgxpool.Pool) {
	t.Helper()
	(&rlsdb.Harness{AdminPool: admin}).EnsurePgbossStandin(ctx, t)
}

// mustUpsertUser seeds a real user row via the auth_upsert_user SECURITY
// DEFINER function, which bypasses RLS for setup exactly as production OAuth
// signup does (mirrors db/tests/cv_ingestions_rls.test.sql).
func mustUpsertUser(ctx context.Context, t *testing.T, pool *pgxpool.Pool, email, googleID string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(ctx,
		`SELECT id FROM auth_upsert_user($1, $2, NULL)`, email, googleID,
	).Scan(&id)
	require.NoError(t, err, "seed user via auth_upsert_user")
	return id
}

type cvIngestionRow struct {
	ID     uuid.UUID
	UserID uuid.UUID
}

func mustGetIngestionRow(ctx context.Context, t *testing.T, pool *pgxpool.Pool, runID uuid.UUID) cvIngestionRow {
	t.Helper()
	var row cvIngestionRow
	err := pool.QueryRow(ctx, `SELECT id, user_id FROM cv_ingestions WHERE id = $1`, runID).Scan(&row.ID, &row.UserID)
	require.NoError(t, err)
	return row
}

func countCVIngestions(ctx context.Context, t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) int {
	t.Helper()
	var count int
	err := pool.QueryRow(ctx, `SELECT count(*) FROM cv_ingestions WHERE user_id = $1`, userID).Scan(&count)
	require.NoError(t, err)
	return count
}

func cleanupUsage(ctx context.Context, t *testing.T, pool *pgxpool.Pool, userID uuid.UUID, month string) {
	t.Helper()
	_, err := pool.Exec(ctx, `DELETE FROM usage WHERE user_id = $1 AND month = $2`, userID, month)
	require.NoError(t, err)
}

func seedEvaluationsCount(ctx context.Context, t *testing.T, pool *pgxpool.Pool, userID uuid.UUID, month string, count int) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO usage (user_id, month, evaluations_count)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, month) DO UPDATE SET evaluations_count = $3
	`, userID, month, count)
	require.NoError(t, err)
}

func assertIngestionsCount(ctx context.Context, t *testing.T, pool *pgxpool.Pool, userID uuid.UUID, month string, want int32) {
	t.Helper()
	var got int32
	err := pool.QueryRow(ctx,
		`SELECT ingestions_count FROM usage WHERE user_id = $1 AND month = $2`, userID, month,
	).Scan(&got)
	require.NoError(t, err)
	require.Equal(t, want, got, "usage.ingestions_count mismatch")
}

func assertEvaluationsCount(ctx context.Context, t *testing.T, pool *pgxpool.Pool, userID uuid.UUID, month string, want int32) {
	t.Helper()
	var got int32
	err := pool.QueryRow(ctx,
		`SELECT evaluations_count FROM usage WHERE user_id = $1 AND month = $2`, userID, month,
	).Scan(&got)
	require.NoError(t, err)
	require.Equal(t, want, got, "usage.evaluations_count mismatch")
}
