package platform_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
	"github.com/miguel-anay/career-ops-saas/api/internal/platform"
	"github.com/stretchr/testify/require"
)

// TestWithTenantTx_RLS_Integration proves platform.WithTenantTx actually
// sets app.current_user_id via set_config(..., true) before fn runs, so RLS
// policies on tenant tables are enforced for the duration of fn, and that
// the transaction commits on a nil return / rolls back on an error return.
//
// Generalized from the proven cv.withTenant precedent (api/internal/cv/service.go)
// — this is the Seam-1 foundation helper every domain service will call.
//
// Skips cleanly when TEST_DATABASE_URL is unset. To run against a live DB:
//
//	TEST_DATABASE_URL="postgres://app_user:app_pw@localhost:5432/careerops?sslmode=disable" \
//	  go test ./internal/platform/... -run TestWithTenantTx_RLS_Integration -v
//
// Requires db/migrations/001_initial.sql + 002_ingest_cv.sql + 003_rls_nullif.sql applied.
func TestWithTenantTx_RLS_Integration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run platform.WithTenantTx integration tests")
	}
	adminDSN := os.Getenv("TEST_ADMIN_DATABASE_URL")
	if adminDSN == "" {
		adminDSN = strings.Replace(dsn, "app_user:app_pw", "careerops:careerops", 1)
	}

	ctx := context.Background()

	appPool, err := platform.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer appPool.Close()

	adminPool, err := platform.NewPool(ctx, adminDSN)
	require.NoError(t, err, "admin connection (set TEST_ADMIN_DATABASE_URL for a superuser/owner DSN)")
	defer adminPool.Close()

	userA := mustUpsertUser(ctx, t, adminPool, "withtenanttx-itest-a@test.invalid", "withtenanttx_itest_google_a")
	userB := mustUpsertUser(ctx, t, adminPool, "withtenanttx-itest-b@test.invalid", "withtenanttx_itest_google_b")

	var jobAID uuid.UUID
	err = platform.WithTenantTx(ctx, appPool, userA, func(q *db.Queries) error {
		job, err := q.InsertJob(ctx, db.InsertJobParams{
			UserID:  userA,
			Title:   "WithTenantTx Test Job",
			Company: "Acme Corp",
			Url:     "https://boards.greenhouse.io/acme/jobs/withtenanttx-" + uuid.New().String(),
			Status:  db.JobStatusTNew,
		})
		if err != nil {
			return err
		}
		jobAID = job.ID
		return nil
	})
	require.NoError(t, err, "insert under tenant A's tx must succeed")
	require.NotEqual(t, uuid.Nil, jobAID)

	t.Run("GUC-scoped query returns only the matching tenant's rows", func(t *testing.T) {
		var gotJob db.Job
		err := platform.WithTenantTx(ctx, appPool, userA, func(q *db.Queries) error {
			row, err := q.GetJobByID(ctx, jobAID)
			gotJob = row
			return err
		})
		require.NoError(t, err, "user A must see their own job under their own tenant tx")
		require.Equal(t, jobAID, gotJob.ID)
		require.Equal(t, userA, gotJob.UserID)

		err = platform.WithTenantTx(ctx, appPool, userB, func(q *db.Queries) error {
			_, err := q.GetJobByID(ctx, jobAID)
			return err
		})
		require.Error(t, err, "user B must NOT see user A's job under B's tenant tx (RLS denial)")
		require.True(t, errors.Is(err, sql.ErrNoRows), "expected sql.ErrNoRows from RLS-filtered SELECT, got: %v", err)
	})

	t.Run("commits on fn returning nil", func(t *testing.T) {
		var insertedID uuid.UUID
		err := platform.WithTenantTx(ctx, appPool, userA, func(q *db.Queries) error {
			job, err := q.InsertJob(ctx, db.InsertJobParams{
				UserID:  userA,
				Title:   "Commit Test Job",
				Company: "Acme Corp",
				Url:     "https://boards.greenhouse.io/acme/jobs/commit-" + uuid.New().String(),
				Status:  db.JobStatusTNew,
			})
			insertedID = job.ID
			return err
		})
		require.NoError(t, err)

		// Verify the row is visible in a fresh tenant tx (i.e. it was actually committed).
		var found db.Job
		err = platform.WithTenantTx(ctx, appPool, userA, func(q *db.Queries) error {
			row, err := q.GetJobByID(ctx, insertedID)
			found = row
			return err
		})
		require.NoError(t, err)
		require.Equal(t, insertedID, found.ID)
	})

	t.Run("rolls back on fn returning an error", func(t *testing.T) {
		sentinelErr := errors.New("boom")
		var insertedID uuid.UUID
		err := platform.WithTenantTx(ctx, appPool, userA, func(q *db.Queries) error {
			job, err := q.InsertJob(ctx, db.InsertJobParams{
				UserID:  userA,
				Title:   "Rollback Test Job",
				Company: "Acme Corp",
				Url:     "https://boards.greenhouse.io/acme/jobs/rollback-" + uuid.New().String(),
				Status:  db.JobStatusTNew,
			})
			if err != nil {
				return err
			}
			insertedID = job.ID
			return sentinelErr
		})
		require.ErrorIs(t, err, sentinelErr)

		// The insert must NOT be visible — the transaction was rolled back.
		var count int
		require.NoError(t, adminPool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE id = $1`, insertedID).Scan(&count))
		require.Equal(t, 0, count, "rolled-back insert must not be persisted")
	})
}

// mustUpsertUser seeds a real user row via the auth_upsert_user SECURITY
// DEFINER function, which bypasses RLS for setup exactly as production OAuth
// signup does (mirrors api/internal/cv/ingest_integration_test.go).
func mustUpsertUser(ctx context.Context, t *testing.T, pool *pgxpool.Pool, email, googleID string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(ctx,
		`SELECT id FROM auth_upsert_user($1, $2, NULL)`, email, googleID,
	).Scan(&id)
	require.NoError(t, err, "seed user via auth_upsert_user")
	return id
}
