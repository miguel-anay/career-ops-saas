package rlsdb_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
	"github.com/miguel-anay/career-ops-saas/api/internal/platform"
	"github.com/miguel-anay/career-ops-saas/api/internal/testsupport/rlsdb"
	"github.com/stretchr/testify/require"
)

// TestHarness_WithTenantTx_CrossTenantDenial proves the rlsdb harness itself
// works end-to-end with platform.WithTenantTx: user A's tenant tx sees only
// A's rows, never B's. This is the foundation-level proof every Seam 2-8
// per-domain integration test builds on.
func TestHarness_WithTenantTx_CrossTenantDenial(t *testing.T) {
	ctx := context.Background()
	h := rlsdb.New(ctx, t)

	userA := h.SeedUser(ctx, t, "rlsdb-harness-a@test.invalid", "rlsdb_harness_google_a")
	userB := h.SeedUser(ctx, t, "rlsdb-harness-b@test.invalid", "rlsdb_harness_google_b")

	var jobID uuid.UUID
	err := platform.WithTenantTx(ctx, h.AppPool, userA, func(q *db.Queries) error {
		job, err := q.InsertJob(ctx, db.InsertJobParams{
			UserID:  userA,
			Title:   "Harness Test Job",
			Company: "Acme Corp",
			Url:     "https://boards.greenhouse.io/acme/jobs/harness-" + uuid.New().String(),
			Status:  db.JobStatusTNew,
		})
		if err != nil {
			return err
		}
		jobID = job.ID
		return nil
	})
	require.NoError(t, err)

	t.Run("owner sees their own row", func(t *testing.T) {
		var got db.Job
		err := platform.WithTenantTx(ctx, h.AppPool, userA, func(q *db.Queries) error {
			row, err := q.GetJobByID(ctx, jobID)
			got = row
			return err
		})
		require.NoError(t, err)
		require.Equal(t, jobID, got.ID)
	})

	t.Run("non-owner is denied (RLS, not app-layer)", func(t *testing.T) {
		err := platform.WithTenantTx(ctx, h.AppPool, userB, func(q *db.Queries) error {
			_, err := q.GetJobByID(ctx, jobID)
			return err
		})
		require.Error(t, err)
		require.True(t, errors.Is(err, sql.ErrNoRows))
	})
}

// TestHarness_EmptyGUC_DeniesCleanly proves that, AFTER migration
// 003_rls_nullif.sql is applied, a connection whose app.current_user_id GUC
// has reset to '' (the pooled-connection state once a prior tenant tx
// ended) denies cleanly instead of raising 22P02 invalid input syntax for
// type uuid. This is the Go-level mirror of db/tests/nullif_guc.test.sql,
// exercised through the same AppPool every domain service uses.
func TestHarness_EmptyGUC_DeniesCleanly(t *testing.T) {
	ctx := context.Background()
	h := rlsdb.New(ctx, t)

	conn, err := h.AppPool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	_, err = conn.Exec(ctx, "SELECT set_config('app.current_user_id', '', false)")
	require.NoError(t, err)

	var count int
	err = conn.QueryRow(ctx, "SELECT count(*) FROM jobs").Scan(&count)
	require.NoError(t, err, "empty-string GUC must deny cleanly (0 rows), not raise 22P02 (run AFTER migration 003)")
	require.Equal(t, 0, count)
}
