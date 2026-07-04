-- Run locally: make test-rls
-- Run manually (as app_user, the non-superuser runtime role, so FORCE RLS is
-- actually exercised — connecting as a superuser bypasses RLS unconditionally
-- regardless of FORCE ROW LEVEL SECURITY and would make this test a false
-- positive):
--   PGPASSWORD=app_pw pg_prove -U app_user -d careerops db/tests/gmail_ingestion_rls.test.sql
-- Requires: pgTAP extension + live PostgreSQL
--
-- pgTAP RLS isolation tests for gmail-job-ingestion PR 1 (T-218): proves
-- users.google_refresh_token exists (nullable, no default), and
-- email_ingest_runs forces RLS with a tenant policy that denies cross-tenant
-- SELECT — mirroring db/tests/cv_ingestions_rls.test.sql conventions.
--
-- Prerequisites:
--   1. pgTAP extension must be installed: CREATE EXTENSION pgtap;
--   2. Run as the app_user role (which has FORCE ROW LEVEL SECURITY applied
--      and does NOT bypass RLS, unlike the superuser role used to own the DB)
--   3. db/migrations/006_gmail_ingestion.sql (or the equivalent schema.sql)
--      must be applied

BEGIN;
SELECT plan(6);

-- ============================================================
-- Column assertion: users.google_refresh_token exists, nullable text
-- ============================================================
SELECT has_column('users', 'google_refresh_token',
  'users.google_refresh_token column exists');

SELECT col_is_null('users', 'google_refresh_token',
  'users.google_refresh_token is nullable (existing users have none until they connect Gmail)');

-- ============================================================
-- Setup: create two independent users via the SECURITY DEFINER helper
-- (auth_upsert_user bypasses RLS for setup exactly as it does in production
-- OAuth signup; this lets the test run as app_user end-to-end so FORCE RLS
-- is genuinely exercised by the assertions below)
-- ============================================================
DO $$
DECLARE
  v_user_a  users;
  v_user_b  users;
  v_run_id  uuid;
BEGIN
  SELECT * INTO v_user_a FROM auth_upsert_user('gmail-itest-a@test.invalid', 'gmail_itest_google_a', NULL);
  SELECT * INTO v_user_b FROM auth_upsert_user('gmail-itest-b@test.invalid', 'gmail_itest_google_b', NULL);
  PERFORM set_config('test.user_a', v_user_a.id::text, false);
  PERFORM set_config('test.user_b', v_user_b.id::text, false);

  -- Seed an email_ingest_runs row as user A
  PERFORM set_config('app.current_user_id', v_user_a.id::text, false);
  INSERT INTO email_ingest_runs (user_id, status)
  VALUES (v_user_a.id, 'running')
  RETURNING id INTO v_run_id;
  PERFORM set_config('test.run_id', v_run_id::text, false);
END;
$$;

-- ============================================================
-- Verify FORCE ROW LEVEL SECURITY is active on email_ingest_runs
-- ============================================================
SELECT ok(
  (SELECT relrowsecurity FROM pg_class WHERE relname = 'email_ingest_runs') = true,
  'RLS is enabled on email_ingest_runs table'
);

SELECT ok(
  (SELECT relforcerowsecurity FROM pg_class WHERE relname = 'email_ingest_runs') = true,
  'RLS is FORCED on email_ingest_runs table'
);

-- ============================================================
-- Cross-tenant isolation: switch to user B and verify no rows visible
-- (spec scenario "RLS scoped" / "first-time Gmail connection")
-- ============================================================
SELECT set_config('app.current_user_id', current_setting('test.user_b'), false);

SELECT is(
  (SELECT count(*)::int FROM email_ingest_runs),
  0,
  'User B cannot read User A email_ingest_runs row (SC-06)'
);

SELECT is(
  (SELECT count(*)::int FROM email_ingest_runs WHERE id = current_setting('test.run_id')::uuid),
  0,
  'User B cannot fetch User A email_ingest_run by ID (NFR-01)'
);

SELECT * FROM finish();
ROLLBACK;
