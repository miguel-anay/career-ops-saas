package jobs_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/jobs"
	"github.com/miguel-anay/career-ops-saas/api/internal/testsupport/rlsdb"
	"github.com/stretchr/testify/require"
)

// TestAddManual_EnqueueGate proves the FU-4 follow-up (spec.md's
// "jobs-manual-create" domain, scenarios "Manual job on an allowlisted
// host", "Manual job on a non-allowlisted host", and "Enqueue failure after
// a successful upsert"): AddManual's fetch-job-content enqueue is gated on
// lookupAllowedHost, but the job upsert always succeeds regardless of host,
// and an enqueue error never rolls back the already-created job row.
//
// Mocked Servicer tests (handler_test.go) only prove the HTTP layer calls
// AddManual — they cannot prove the enqueue gate itself, since the mock
// never runs the real Service. This drives jobs.Service.AddManual directly
// against a real Postgres connection (RLS-enforced app_user pool) via the
// shared rlsdb harness, exactly as rls_integration_test.go does for
// GetByID/List.
//
// DB-gated: skips cleanly when TEST_DATABASE_URL is unset (see rlsdb.New).
func TestAddManual_EnqueueGate(t *testing.T) {
	ctx := context.Background()

	t.Run("allowlisted host: job stored AND fetch-job-content enqueued", func(t *testing.T) {
		h := rlsdb.New(ctx, t)
		h.EnsurePgbossSchema(ctx, t, "fetch-job-content")

		userID := h.SeedUser(ctx, t, "addmanual-itest-allowed@test.invalid", "addmanual_itest_allowed")
		svc := jobs.NewService(h.AppPool)

		rawURL := "https://www.linkedin.com/jobs/view/" + uuid.New().String()
		job, err := svc.AddManual(ctx, userID, rawURL)
		require.NoError(t, err)
		require.NotNil(t, job)
		require.Equal(t, rawURL, job.Url)

		var count int
		err = h.AdminPool.QueryRow(ctx,
			`SELECT count(*) FROM pgboss.job WHERE name = 'fetch-job-content' AND data->>'job_id' = $1`,
			job.ID.String(),
		).Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count, "fetch-job-content must be enqueued for an allowlisted host")
	})

	t.Run("non-allowlisted host: job stored, fetch-job-content NOT enqueued", func(t *testing.T) {
		h := rlsdb.New(ctx, t)
		h.EnsurePgbossSchema(ctx, t, "fetch-job-content")

		userID := h.SeedUser(ctx, t, "addmanual-itest-disallowed@test.invalid", "addmanual_itest_disallowed")
		svc := jobs.NewService(h.AppPool)

		rawURL := "https://careers.example-company.invalid/jobs/" + uuid.New().String()
		job, err := svc.AddManual(ctx, userID, rawURL)
		require.NoError(t, err, "the upsert must succeed regardless of host")
		require.NotNil(t, job)
		require.Equal(t, rawURL, job.Url)

		var count int
		err = h.AdminPool.QueryRow(ctx,
			`SELECT count(*) FROM pgboss.job WHERE name = 'fetch-job-content' AND data->>'job_id' = $1`,
			job.ID.String(),
		).Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 0, count, "fetch-job-content must NOT be enqueued for a non-allowlisted host")
	})

	t.Run("enqueue failure after successful upsert still returns the job", func(t *testing.T) {
		h := rlsdb.New(ctx, t)

		// Deliberately ensure "fetch-job-content" is NOT registered for this
		// assertion (Decision 6: queue.Enqueue fails loudly, not silently, on
		// an unregistered queue) — regardless of whether a sibling subtest
		// already registered it against this same live DB. The partition's
		// FK to pgboss.queue is ON DELETE RESTRICT, which Postgres always
		// checks immediately (RESTRICT, unlike NO ACTION, is never
		// deferrable) — so any rows a sibling subtest enqueued into the
		// partition must be cleared before pgboss.delete_queue can drop the
		// registration; delete_queue itself is guarded by an existence check
		// since it errors (EXECUTE of a null command) when called for a name
		// that was never registered. Restore registration afterward so this
		// test never leaves the DB in a state where the (already-shipped)
		// production enqueue path is broken.
		_, err := h.AdminPool.Exec(ctx, `
			DELETE FROM pgboss.job WHERE name = 'fetch-job-content';
			DO $$
			BEGIN
				IF EXISTS (SELECT 1 FROM pgboss.queue WHERE name = 'fetch-job-content') THEN
					PERFORM pgboss.delete_queue('fetch-job-content');
				END IF;
			END $$;
		`)
		require.NoError(t, err, "clear any pre-existing fetch-job-content queue registration")
		t.Cleanup(func() {
			h.EnsurePgbossSchema(ctx, t, "fetch-job-content")
		})

		userID := h.SeedUser(ctx, t, "addmanual-itest-enqueue-fail@test.invalid", "addmanual_itest_enqueue_fail")
		svc := jobs.NewService(h.AppPool)

		rawURL := "https://www.bumeran.com.pe/empleos/" + uuid.New().String()
		job, err := svc.AddManual(ctx, userID, rawURL)
		require.Error(t, err, "AddManual must surface the enqueue error")
		require.NotNil(t, job, "the already-upserted job must still be returned alongside the enqueue error")
		require.Equal(t, rawURL, job.Url)
	})
}
