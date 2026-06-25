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

// TestHarness_EnsurePgbossSchema_RegistersQueue proves the harness
// provisions the REAL pg-boss v10 partitioned schema (not the old
// hand-rolled flat stand-in) and registers a queue by name, so a job can
// actually be routed into a partition by name afterward. This is the RED
// test for the EnsurePgbossSchema rewrite (was EnsurePgbossStandin) — it
// references methods/columns that only exist once the real v10 schema is
// installed (pgboss.queue, snake_case pgboss.job columns).
func TestHarness_EnsurePgbossSchema_RegistersQueue(t *testing.T) {
	ctx := context.Background()
	h := rlsdb.New(ctx, t)

	h.EnsurePgbossSchema(ctx, t, "rlsdb-harness-test-queue")

	// pgboss.queue is the v10 registry table — it does not exist at all in
	// the old hand-rolled flat fixture, so this assertion only passes
	// against the real schema.
	var policy string
	err := h.AdminPool.QueryRow(ctx,
		`SELECT policy FROM pgboss.queue WHERE name = $1`, "rlsdb-harness-test-queue",
	).Scan(&policy)
	require.NoError(t, err, "queue must be registered in the real pgboss.queue table")
	require.Equal(t, "standard", policy, "createQueue default policy must be 'standard'")

	t.Run("calling again with the same queue name is idempotent", func(t *testing.T) {
		require.NotPanics(t, func() {
			h.EnsurePgbossSchema(ctx, t, "rlsdb-harness-test-queue")
		})
	})

	t.Run("registering a second distinct queue name does not remove the first", func(t *testing.T) {
		h.EnsurePgbossSchema(ctx, t, "rlsdb-harness-test-queue-2")

		var count int
		err := h.AdminPool.QueryRow(ctx,
			`SELECT count(*) FROM pgboss.queue WHERE name IN ($1, $2)`,
			"rlsdb-harness-test-queue", "rlsdb-harness-test-queue-2",
		).Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 2, count)
	})
}

// TestHarness_EmptyGUC_DeniesCleanly proves that, AFTER migration
// 003_rls_nullif.sql is applied, a connection whose app.current_user_id GUC
// has reset to ” (the pooled-connection state once a prior tenant tx
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
