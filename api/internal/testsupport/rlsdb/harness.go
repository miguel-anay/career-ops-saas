// Package rlsdb provides a shared DB-gated test harness for proving
// per-domain RLS enforcement against a live, non-superuser app_user
// connection — generalized from api/internal/cv/ingest_integration_test.go
// (the two-pool template established by that test).
//
// Every Seam 2-8 *_rls_integration_test.go file consumes this package so
// the boilerplate (DSN derivation, pgboss stand-in, user seeding) lives in
// one audited place instead of being copy-pasted across 7 domains.
package rlsdb

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/miguel-anay/career-ops-saas/api/internal/platform"
	"github.com/stretchr/testify/require"
)

// Harness holds the two pools every RLS integration test needs:
//   - AppPool   (app_user, RLS ENFORCED) — exercises the Service under test
//     so the RLS assertions are truthful; a superuser would bypass RLS and
//     make them false positives.
//   - AdminPool (table owner / superuser, creds swapped in the DSN) — seeds
//     fixtures and asserts ground truth independent of RLS.
type Harness struct {
	AppPool   *pgxpool.Pool
	AdminPool *pgxpool.Pool
}

// New returns a Harness connected to TEST_DATABASE_URL (app_user) and
// TEST_ADMIN_DATABASE_URL (or a derived superuser DSN if unset). Calls
// t.Skip when TEST_DATABASE_URL is unset, so callers can call New
// unconditionally at the top of a test function.
//
// The admin DSN defaults to the same host with the careerops superuser
// creds (matching docker-compose.yml); override with
// TEST_ADMIN_DATABASE_URL.
//
// The target DB must have migrations 001_initial.sql, 002_ingest_cv.sql,
// and 003_rls_nullif.sql applied, connecting as the app_user role for
// TEST_DATABASE_URL.
func New(ctx context.Context, t *testing.T) *Harness {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run RLS integration tests")
	}
	adminDSN := os.Getenv("TEST_ADMIN_DATABASE_URL")
	if adminDSN == "" {
		adminDSN = strings.Replace(dsn, "app_user:app_pw", "careerops:careerops", 1)
	}

	appPool, err := platform.NewPool(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(appPool.Close)

	adminPool, err := platform.NewPool(ctx, adminDSN)
	require.NoError(t, err, "admin connection (set TEST_ADMIN_DATABASE_URL for a superuser/owner DSN)")
	t.Cleanup(adminPool.Close)

	return &Harness{AppPool: appPool, AdminPool: adminPool}
}

// SeedUser creates a real user row via the auth_upsert_user SECURITY
// DEFINER function, which bypasses RLS for setup exactly as production
// OAuth signup does.
func (h *Harness) SeedUser(ctx context.Context, t *testing.T, email, googleID string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := h.AdminPool.QueryRow(ctx,
		`SELECT id FROM auth_upsert_user($1, $2, NULL)`, email, googleID,
	).Scan(&id)
	require.NoError(t, err, "seed user via auth_upsert_user")
	return id
}

// pgbossStandinLockID is an arbitrary fixed key for a Postgres advisory
// lock. Multiple test packages call EnsurePgbossSchema concurrently (Go
// runs packages in parallel by default) against a cold test database with
// no pgboss schema yet; the construction DDL and pgboss.create_queue()
// (CREATE TABLE ... ATTACH PARTITION) deadlock (SQLSTATE 40P01) if run
// concurrently from multiple connections, not merely race with "tuple
// concurrently updated". EnsurePgbossSchema acquires this as a
// SESSION-scoped lock (pg_advisory_lock/pg_advisory_unlock) on ONE
// connection explicitly Acquire()'d from AdminPool — held for the FULL
// install+createQueue+grant sequence, then released — so the entire
// operation is serialized across every concurrent caller.
const pgbossStandinLockID = 0x70676220 // "pgb " in hex, arbitrary

// pgbossSchemaSQL is the REAL pg-boss v10 construction DDL, dumped verbatim
// from pg-boss's own PgBoss.getConstructionPlans('pgboss') static method by
// worker/scripts/dump-pgboss-schema.mjs (see db/pgboss_schema.generated.sql
// for the regeneration command and provenance). It is loaded once per test
// binary, lazily, the first time EnsurePgbossSchema runs.
//
// Using pg-boss's own generated DDL — rather than hand-transcribing the
// partitioned job table / queue registry / create_queue() function into Go
// — means this fixture cannot silently drift from the real schema on a
// pg-boss version bump; regenerating the committed SQL file is the only
// way to update it, and that regeneration is reviewable in the same diff
// as the package.json version bump.
var pgbossSchemaSQL string

func loadPgbossSchemaSQL(t *testing.T) string {
	t.Helper()
	if pgbossSchemaSQL != "" {
		return pgbossSchemaSQL
	}

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "resolve harness.go source path via runtime.Caller")
	// thisFile = .../api/internal/testsupport/rlsdb/harness.go
	// repo root = five levels up (rlsdb -> testsupport -> internal -> api -> repo root)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	ddlPath := filepath.Join(repoRoot, "db", "pgboss_schema.generated.sql")

	raw, err := os.ReadFile(ddlPath)
	require.NoError(t, err, "read generated pg-boss schema DDL at %s (run: node worker/scripts/dump-pgboss-schema.mjs)", ddlPath)

	pgbossSchemaSQL = string(raw)
	return pgbossSchemaSQL
}

// EnsurePgbossSchema installs the REAL pg-boss v10 partitioned schema
// (pgboss.job, pgboss.queue, pgboss.create_queue(), etc. — generated
// verbatim from pg-boss's own getConstructionPlans, see
// db/pgboss_schema.generated.sql) against the test database, then
// registers queueName via pgboss.create_queue() so queue.Enqueue's INSERT
// (which JOINs against pgboss.queue) can actually route a job into a
// partition.
//
// This replaces the old EnsurePgbossStandin hand-rolled flat table — the
// real schema's JOIN-against-registered-queue contract is exactly what the
// rewritten queue.Enqueue (api/internal/queue/boss.go) now depends on, and
// what the original incident's fake table never exercised.
//
// Concurrency: Go runs test packages in parallel by default, and every
// *_rls_integration_test.go across scan/evaluate/cv calls this on a COLD
// test database that has no pgboss schema yet. The construction DDL
// (CREATE TABLE/TYPE/FUNCTION) and pgboss.create_queue() (which itself runs
// CREATE TABLE ... ATTACH PARTITION) are not safe to run concurrently from
// multiple connections — two packages racing to install/ATTACH PARTITION
// at once deadlock (Postgres SQLSTATE 40P01), not merely "tuple
// concurrently updated". A SESSION-scoped advisory lock (not
// xact-scoped) held on ONE dedicated connection, acquired explicitly via
// AdminPool.Acquire and released in a deferred unlock, serializes the
// ENTIRE install-DDL + create_queue + grant sequence across all callers —
// the xact-scoped lock used by the old EnsurePgbossStandin only protected
// itself, not concurrent calls racing on a pool with multiple connections.
func (h *Harness) EnsurePgbossSchema(ctx context.Context, t *testing.T, queueName string) {
	t.Helper()

	conn, err := h.AdminPool.Acquire(ctx)
	require.NoError(t, err, "acquire dedicated connection for pgboss schema install lock")
	defer conn.Release()

	_, err = conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, pgbossStandinLockID)
	require.NoError(t, err, "acquire pgboss schema install session advisory lock")
	defer func() {
		_, unlockErr := conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, pgbossStandinLockID)
		require.NoError(t, unlockErr, "release pgboss schema install session advisory lock")
	}()

	// Guard: only run the full construction DDL if pgboss.queue does not
	// exist yet (i.e. v10 was never installed in this test DB). The DDL
	// itself is BEGIN/COMMIT-wrapped (from getConstructionPlans), so it
	// must run on its own statement (not nested inside another tx) — this
	// is safe now because the session lock above already serializes all
	// concurrent callers.
	var alreadyInstalled bool
	err = conn.QueryRow(ctx, `SELECT to_regclass('pgboss.queue') IS NOT NULL`).Scan(&alreadyInstalled)
	require.NoError(t, err, "check whether pg-boss v10 schema is already installed")

	if !alreadyInstalled {
		ddl := loadPgbossSchemaSQL(t)
		_, err = conn.Exec(ctx, ddl)
		require.NoError(t, err, "install real pg-boss v10 schema from db/pgboss_schema.generated.sql")
	}

	// Register the queue (admin-side, exactly as production's
	// worker/scripts/install-pgboss.mjs does — never delegated to
	// app_user). create_queue() ON CONFLICT DO NOTHING is idempotent.
	// Still serialized by the same session lock — concurrent
	// createQueue calls for DIFFERENT names also race on ATTACH PARTITION
	// against the shared pgboss.job parent table.
	options, err := json.Marshal(map[string]any{"policy": "standard"})
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `SELECT pgboss.create_queue($1, $2::json)`, queueName, string(options))
	require.NoError(t, err, "register queue %q via pgboss.create_queue", queueName)

	// Grant app_user DML on the schema + this queue's new partition table
	// (db/pgboss_grants.sql covers this for production; tests re-apply the
	// same grants here since ALTER DEFAULT PRIVILEGES only fires for
	// objects created by the role that ran it, i.e. whichever role ran
	// CREATE EXTENSION/migrations — re-granting per-call is the simplest
	// correct fixture behavior).
	_, err = conn.Exec(ctx, `
		GRANT USAGE ON SCHEMA pgboss TO app_user;
		GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA pgboss TO app_user;
		GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA pgboss TO app_user;
	`)
	require.NoError(t, err, "grant app_user DML on pgboss schema")
}
