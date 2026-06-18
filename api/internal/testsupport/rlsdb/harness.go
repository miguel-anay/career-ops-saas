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
	"os"
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

// EnsurePgbossStandin creates a minimal pgboss.job table matching the
// columns queue.Enqueue inserts, and grants app_user INSERT/SELECT, so the
// enqueue path runs against a bare migrated DB. The real schema is created
// by the pg-boss runtime at worker boot; this is a test fixture only.
func (h *Harness) EnsurePgbossStandin(ctx context.Context, t *testing.T) {
	t.Helper()
	const ddl = `
		CREATE SCHEMA IF NOT EXISTS pgboss;
		CREATE TABLE IF NOT EXISTS pgboss.job (
			id          uuid PRIMARY KEY,
			name        text NOT NULL,
			data        jsonb,
			state       text NOT NULL DEFAULT 'created',
			"createdOn" timestamptz NOT NULL DEFAULT now(),
			"startAfter" timestamptz NOT NULL DEFAULT now(),
			"expireIn"  interval NOT NULL DEFAULT interval '15 minutes',
			priority    integer NOT NULL DEFAULT 0
		);
		GRANT USAGE ON SCHEMA pgboss TO app_user;
		GRANT INSERT, SELECT ON pgboss.job TO app_user;`
	_, err := h.AdminPool.Exec(ctx, ddl)
	require.NoError(t, err, "create pgboss stand-in schema")
}
