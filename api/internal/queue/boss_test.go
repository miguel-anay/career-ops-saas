package queue_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/miguel-anay/career-ops-saas/api/internal/queue"
	"github.com/miguel-anay/career-ops-saas/api/internal/testsupport/rlsdb"
	"github.com/stretchr/testify/require"
)

// TestEnqueue_RealV10Schema_Integration is the primary acceptance test for
// the pgboss-queue-unification change: it proves queue.Enqueue's hand-
// replicated SQL contract (api/internal/queue/boss.go) actually lands a
// row in the REAL pg-boss v10 partitioned schema when the target queue is
// registered, and FAILS LOUDLY (returns a non-nil error) when it is not.
//
// This is the gate that would have caught the original incident: the old
// fixture (EnsurePgbossStandin) built a hand-rolled flat table that bore no
// resemblance to the v10 schema the worker actually runs against, so this
// exact failure mode (queue.Enqueue silently inserting into a schema the
// worker could never read from) was never exercised by any test.
//
// Skips cleanly when TEST_DATABASE_URL is unset. To run against a live DB:
//
//	TEST_DATABASE_URL="postgres://app_user:app_pw@localhost:5432/careerops?sslmode=disable" \
//	  go test ./internal/queue/... -run TestEnqueue_RealV10Schema_Integration -v
func TestEnqueue_RealV10Schema_Integration(t *testing.T) {
	ctx := context.Background()
	h := rlsdb.New(ctx, t)

	t.Run("registered queue: Enqueue lands a row routed into the correct partition", func(t *testing.T) {
		h.EnsurePgbossSchema(ctx, t, "queue-acceptance-registered")

		payload := json.RawMessage(`{"acceptance":"registered-queue-case"}`)
		err := queue.Enqueue(ctx, h.AppPool, queue.Job{
			Name: "queue-acceptance-registered",
			Data: payload,
		})
		require.NoError(t, err, "Enqueue must succeed against a registered queue on the real v10 schema")

		// Read back via the parent partitioned pgboss.job table — proves the
		// row actually landed in the partition for this queue name, not just
		// "no error was returned". Reading through the PARENT table (rather
		// than the per-queue partition table directly) is itself part of
		// what's being proven: pg-boss's own consumers (boss.fetch/work) also
		// read through pgboss.job, relying on partition routing by name.
		var gotName string
		var gotData []byte
		err = h.AdminPool.QueryRow(ctx,
			`SELECT name, data FROM pgboss.job WHERE name = $1 ORDER BY created_on DESC LIMIT 1`,
			"queue-acceptance-registered",
		).Scan(&gotName, &gotData)
		require.NoError(t, err, "row must be readable back from pgboss.job after Enqueue")
		require.Equal(t, "queue-acceptance-registered", gotName)
		require.JSONEq(t, string(payload), string(gotData), "job.data must round-trip exactly as enqueued")
	})

	t.Run("unregistered queue: Enqueue fails loudly instead of silently inserting nothing", func(t *testing.T) {
		// Deliberately do NOT call EnsurePgbossSchema for this name — this is
		// the silent-failure trap pg-boss's own createJob() swallows
		// (manager.js:380-382 returns null with no exception). queue.Enqueue
		// must surface it as a Go error instead.
		err := queue.Enqueue(ctx, h.AppPool, queue.Job{
			Name: "queue-acceptance-never-registered",
			Data: json.RawMessage(`{}`),
		})
		require.Error(t, err, "Enqueue must return an error when the queue was never registered via createQueue")
		require.Contains(t, err.Error(), "queue-acceptance-never-registered")

		var count int
		countErr := h.AdminPool.QueryRow(ctx,
			`SELECT count(*) FROM pgboss.job WHERE name = $1`, "queue-acceptance-never-registered",
		).Scan(&count)
		require.NoError(t, countErr)
		require.Equal(t, 0, count, "no row must be inserted anywhere when the queue is unregistered")
	})

	t.Run("pgboss writes never go through a tenant transaction (no RLS on pgboss.*)", func(t *testing.T) {
		h.EnsurePgbossSchema(ctx, t, "queue-acceptance-raw-pool")

		// Enqueue's signature only accepts *pgxpool.Pool, not a tenant tx
		// helper — this sub-test documents and pins that contract so a future
		// change cannot accidentally route pgboss writes through
		// platform.WithTenantTx (which would set app.current_user_id, a GUC
		// pgboss.* has no policies for and does not need).
		var raw *pgxpool.Pool = h.AppPool
		err := queue.Enqueue(ctx, raw, queue.Job{
			Name: "queue-acceptance-raw-pool",
			Data: json.RawMessage(`{}`),
		})
		require.NoError(t, err)
	})
}

// TestEnqueue_RequiresAdminProvisioning_NotRuntimeWorkerPath documents
// (via a fast unit-level check, no live DB needed) that queue.Enqueue's own
// SQL never attempts to call pgboss.create_queue() — registration is
// strictly an admin/installer-time concern (worker/scripts/install-pgboss.mjs),
// never a runtime path triggered by the app_user-scoped Enqueue call. This
// guards against a future change accidentally adding auto-registration
// inside Enqueue, which would let app_user create partition tables and hit
// the ALTER DEFAULT PRIVILEGES gap documented in the explore/proposal.
func TestEnqueue_RequiresAdminProvisioning_NotRuntimeWorkerPath(t *testing.T) {
	src := readBossGoSource(t)
	require.NotContains(t, src, "create_queue",
		"queue.Enqueue must never call pgboss.create_queue() itself — registration is admin-only, out-of-band (see worker/scripts/install-pgboss.mjs)")
}

func readBossGoSource(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("boss.go")
	require.NoError(t, err)
	return strings.ToLower(string(raw))
}
