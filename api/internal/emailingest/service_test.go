package emailingest_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/emailingest"
	"github.com/miguel-anay/career-ops-saas/api/internal/testsupport/rlsdb"
	"github.com/stretchr/testify/require"
)

// TestTriggerIngest_Integration proves the two spec scenarios "Ingest
// attempted without Gmail token" and "Successful trigger": TriggerIngest
// returns emailingest.ErrGmailNotConnected (and creates no run) when
// users.google_refresh_token is NULL, and inserts an email_ingest_runs row
// plus enqueues an "ingest-email" pgboss job when the token is present.
//
// DB-gated: skips cleanly when TEST_DATABASE_URL is unset (see rlsdb.New).
func TestTriggerIngest_Integration(t *testing.T) {
	ctx := context.Background()
	h := rlsdb.New(ctx, t)
	h.EnsurePgbossSchema(ctx, t, "ingest-email")

	svc := emailingest.NewService(h.AppPool)

	t.Run("no Gmail token: ErrGmailNotConnected, no run created", func(t *testing.T) {
		userID := h.SeedUser(ctx, t, "emailingest-itest-no-token@test.invalid", "emailingest_itest_no_token")

		runID, err := svc.TriggerIngest(ctx, userID)
		require.ErrorIs(t, err, emailingest.ErrGmailNotConnected)
		require.Equal(t, uuid.Nil, runID)

		count := countEmailIngestRuns(ctx, t, h, userID)
		require.Equal(t, 0, count, "no email_ingest_runs row must be created when Gmail is not connected")
	})

	t.Run("Gmail token present: inserts run and enqueues ingest-email job", func(t *testing.T) {
		userID := h.SeedUser(ctx, t, "emailingest-itest-connected@test.invalid", "emailingest_itest_connected")
		mustSetGoogleRefreshToken(ctx, t, h, userID, "refresh-token-value")

		runID, err := svc.TriggerIngest(ctx, userID)
		require.NoError(t, err)
		require.NotEqual(t, uuid.Nil, runID)

		row := mustGetEmailIngestRunRow(ctx, t, h, runID)
		require.Equal(t, userID, row.UserID)
		require.Equal(t, "running", row.Status)

		jobCount := countPgbossJobs(ctx, t, h, "ingest-email")
		require.GreaterOrEqual(t, jobCount, 1, "TriggerIngest must enqueue an ingest-email job")
	})

	// TestTriggerIngest_Integration/enqueue fails after run insert proves the
	// review finding fix: an enqueue failure AFTER the email_ingest_runs row
	// is committed must not leave that row orphaned in status "running"
	// forever (no worker job will ever exist to finalize it). The queue is
	// temporarily unregistered (DELETE FROM pgboss.queue) so queue.Enqueue's
	// JOIN yields zero rows and fails deterministically — the same
	// silent-failure trap proven in queue/boss_test.go — then re-registered
	// via t.Cleanup so later subtests/tests are unaffected.
	t.Run("enqueue fails after run insert: run is marked error, not left running", func(t *testing.T) {
		userID := h.SeedUser(ctx, t, "emailingest-itest-enqueue-fail@test.invalid", "emailingest_itest_enqueue_fail")
		mustSetGoogleRefreshToken(ctx, t, h, userID, "refresh-token-value")

		// q_fkey is ON DELETE RESTRICT, which Postgres checks immediately
		// regardless of DEFERRABLE — any existing job rows for this queue
		// (e.g. from the "Gmail token present" subtest above) must be
		// cleared first. pgboss.delete_queue is pg-boss's own admin function:
		// it then removes the registry row AND drops the queue's job
		// partition table together, so t.Cleanup's EnsurePgbossSchema can
		// recreate a clean partition afterward. This leaves queue.Enqueue's
		// JOIN against pgboss.queue matching zero rows — the same
		// silent-failure trap proven in queue/boss_test.go.
		_, err := h.AdminPool.Exec(ctx, `DELETE FROM pgboss.job WHERE name = $1`, "ingest-email")
		require.NoError(t, err, "clear existing ingest-email job rows")
		_, err = h.AdminPool.Exec(ctx, `SELECT pgboss.delete_queue($1)`, "ingest-email")
		require.NoError(t, err, "temporarily unregister ingest-email queue")
		t.Cleanup(func() { h.EnsurePgbossSchema(ctx, t, "ingest-email") })

		runID, err := svc.TriggerIngest(ctx, userID)
		require.Error(t, err, "TriggerIngest must surface the enqueue failure")
		require.Equal(t, uuid.Nil, runID)

		row := mustGetLatestEmailIngestRunForUser(ctx, t, h, userID)
		require.Equal(t, "error", row.Status,
			"run must be marked error on enqueue failure, not left running forever")
	})
}

// TestGetIngestRun_Integration proves RLS isolation for reads: a
// non-owner's GetIngestRun call is denied at the DB layer, and the owner's
// call still succeeds.
func TestGetIngestRun_Integration(t *testing.T) {
	ctx := context.Background()
	h := rlsdb.New(ctx, t)

	userA := h.SeedUser(ctx, t, "emailingest-itest-a@test.invalid", "emailingest_itest_a")
	userB := h.SeedUser(ctx, t, "emailingest-itest-b@test.invalid", "emailingest_itest_b")

	svc := emailingest.NewService(h.AppPool)

	runA := mustInsertEmailIngestRun(ctx, t, h, userA)

	t.Run("RLS isolation: non-owner GetIngestRun is denied at the DB layer", func(t *testing.T) {
		_, err := svc.GetIngestRun(ctx, userB, runA)
		require.Error(t, err)
	})

	t.Run("owner GetIngestRun still succeeds", func(t *testing.T) {
		got, err := svc.GetIngestRun(ctx, userA, runA)
		require.NoError(t, err)
		require.Equal(t, runA, got.ID)
		require.Equal(t, userA, got.UserID)
	})
}

func mustSetGoogleRefreshToken(ctx context.Context, t *testing.T, h *rlsdb.Harness, userID uuid.UUID, token string) {
	t.Helper()
	_, err := h.AdminPool.Exec(ctx, `UPDATE users SET google_refresh_token = $2 WHERE id = $1`, userID, token)
	require.NoError(t, err, "seed users.google_refresh_token")
}

func mustInsertEmailIngestRun(ctx context.Context, t *testing.T, h *rlsdb.Harness, userID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := h.AdminPool.QueryRow(ctx,
		`INSERT INTO email_ingest_runs (user_id) VALUES ($1) RETURNING id`,
		userID,
	).Scan(&id)
	require.NoError(t, err, "seed email_ingest_runs row")
	return id
}

func countEmailIngestRuns(ctx context.Context, t *testing.T, h *rlsdb.Harness, userID uuid.UUID) int {
	t.Helper()
	var count int
	err := h.AdminPool.QueryRow(ctx, `SELECT count(*) FROM email_ingest_runs WHERE user_id = $1`, userID).Scan(&count)
	require.NoError(t, err)
	return count
}

type emailIngestRunRow struct {
	ID     uuid.UUID
	UserID uuid.UUID
	Status string
}

func mustGetEmailIngestRunRow(ctx context.Context, t *testing.T, h *rlsdb.Harness, id uuid.UUID) emailIngestRunRow {
	t.Helper()
	var row emailIngestRunRow
	err := h.AdminPool.QueryRow(ctx, `SELECT id, user_id, status FROM email_ingest_runs WHERE id = $1`, id).
		Scan(&row.ID, &row.UserID, &row.Status)
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

func mustGetLatestEmailIngestRunForUser(ctx context.Context, t *testing.T, h *rlsdb.Harness, userID uuid.UUID) emailIngestRunRow {
	t.Helper()
	var row emailIngestRunRow
	err := h.AdminPool.QueryRow(ctx,
		`SELECT id, user_id, status FROM email_ingest_runs WHERE user_id = $1 ORDER BY started_at DESC LIMIT 1`,
		userID,
	).Scan(&row.ID, &row.UserID, &row.Status)
	require.NoError(t, err)
	return row
}
