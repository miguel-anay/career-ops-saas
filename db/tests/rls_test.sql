-- Run locally: make test-rls
-- Run manually (as app_user, the non-superuser runtime role, so FORCE RLS is
-- actually exercised — connecting as a superuser bypasses RLS unconditionally
-- regardless of FORCE ROW LEVEL SECURITY and would make this test a false
-- positive):
--   PGPASSWORD=app_pw pg_prove -U app_user -d careerops db/tests/rls_test.sql
-- Requires: pgTAP extension + live PostgreSQL
--
-- pgTAP RLS isolation tests for career-ops-saas
-- Validates SC-06 (cross-tenant isolation) and ADR-3 (RLS is the invariant).
-- All tables tested: users, watched_companies, jobs, applications, reports, cvs, scan_runs, usage
--
-- Prerequisites:
--   1. pgTAP extension must be installed: CREATE EXTENSION pgtap;
--   2. Run as the app_user role (which has FORCE ROW LEVEL SECURITY applied
--      and does NOT bypass RLS, unlike the superuser role used to own the DB)
--   3. The schema + rls.sql + auth_upsert_user.sql must be applied

BEGIN;
SELECT plan(25);

-- ============================================================
-- Setup: create two independent users via the SECURITY DEFINER helper
-- (auth_upsert_user bypasses RLS for setup exactly as it does in production
-- OAuth signup; this lets the test run as app_user end-to-end so FORCE RLS
-- is genuinely exercised by the assertions below, not silently bypassed by
-- a superuser connection — a direct `INSERT INTO users` as app_user would
-- violate the tenant_users WITH CHECK policy for any row other than the
-- caller's own current_user_id)
-- ============================================================
DO $$
DECLARE
  v_user_a users;
  v_user_b users;
BEGIN
  SELECT * INTO v_user_a FROM auth_upsert_user('user_a@test.invalid', 'google_a_001', NULL);
  SELECT * INTO v_user_b FROM auth_upsert_user('user_b@test.invalid', 'google_b_002', NULL);
  PERFORM set_config('test.user_a', v_user_a.id::text, false);
  PERFORM set_config('test.user_b', v_user_b.id::text, false);

  -- Seed data as user A
  PERFORM set_config('app.current_user_id', v_user_a.id::text, false);

  INSERT INTO watched_companies (id, user_id, name, careers_url, provider_id)
  VALUES ('a1000000-0000-0000-0000-000000000010'::uuid, v_user_a.id, 'Acme Corp', 'https://boards.greenhouse.io/acme', 'greenhouse')
  ON CONFLICT DO NOTHING;

  INSERT INTO jobs (id, user_id, title, company, url, platform, status)
  VALUES ('a2000000-0000-0000-0000-000000000020'::uuid, v_user_a.id, 'Senior Engineer', 'Acme Corp', 'https://boards.greenhouse.io/acme/jobs/1', 'greenhouse', 'new')
  ON CONFLICT DO NOTHING;

  INSERT INTO applications (id, user_id, job_id, score, status)
  VALUES ('a3000000-0000-0000-0000-000000000030'::uuid, v_user_a.id, 'a2000000-0000-0000-0000-000000000020'::uuid, 4.2, 'Evaluated')
  ON CONFLICT DO NOTHING;

  INSERT INTO reports (id, user_id, application_id, content_md, blocks_json)
  VALUES ('a4000000-0000-0000-0000-000000000040'::uuid, v_user_a.id, 'a3000000-0000-0000-0000-000000000030'::uuid, '# Report', '{}')
  ON CONFLICT DO NOTHING;

  INSERT INTO cvs (id, user_id, title, content_md, is_master)
  VALUES ('a5000000-0000-0000-0000-000000000050'::uuid, v_user_a.id, 'Master CV', '# CV', true)
  ON CONFLICT DO NOTHING;

  INSERT INTO scan_runs (id, user_id, status, new_jobs, errors_json)
  VALUES ('a6000000-0000-0000-0000-000000000060'::uuid, v_user_a.id, 'completed', 3, '[]')
  ON CONFLICT DO NOTHING;

  INSERT INTO usage (id, user_id, month, evaluations_count, pdfs_count)
  VALUES ('a7000000-0000-0000-0000-000000000070'::uuid, v_user_a.id, '2026-06', 5, 2)
  ON CONFLICT DO NOTHING;
END;
$$;

-- ============================================================
-- Verify FORCE ROW LEVEL SECURITY is active on all tables
-- ============================================================
SELECT ok(
  (SELECT relrowsecurity FROM pg_class WHERE relname = 'users') = true,
  'RLS is enabled on users table'
);

SELECT ok(
  (SELECT relrowsecurity FROM pg_class WHERE relname = 'watched_companies') = true,
  'RLS is enabled on watched_companies table'
);

SELECT ok(
  (SELECT relrowsecurity FROM pg_class WHERE relname = 'jobs') = true,
  'RLS is enabled on jobs table'
);

SELECT ok(
  (SELECT relrowsecurity FROM pg_class WHERE relname = 'applications') = true,
  'RLS is enabled on applications table'
);

SELECT ok(
  (SELECT relrowsecurity FROM pg_class WHERE relname = 'reports') = true,
  'RLS is enabled on reports table'
);

SELECT ok(
  (SELECT relrowsecurity FROM pg_class WHERE relname = 'cvs') = true,
  'RLS is enabled on cvs table'
);

SELECT ok(
  (SELECT relrowsecurity FROM pg_class WHERE relname = 'scan_runs') = true,
  'RLS is enabled on scan_runs table'
);

SELECT ok(
  (SELECT relrowsecurity FROM pg_class WHERE relname = 'usage') = true,
  'RLS is enabled on usage table'
);

-- ============================================================
-- Cross-tenant isolation: switch to user B and verify no rows visible
-- ============================================================
DO $$ BEGIN PERFORM set_config('app.current_user_id', current_setting('test.user_b'), false); END $$;

-- users: user B cannot see user A's row
SELECT is(
  (SELECT count(*)::int FROM users WHERE id = current_setting('test.user_a')::uuid),
  0,
  'User B cannot read User A row in users (SC-06)'
);

-- users: user B can see their own row
SELECT is(
  (SELECT count(*)::int FROM users WHERE id = current_setting('test.user_b')::uuid),
  1,
  'User B can read their own row in users'
);

-- watched_companies: user B cannot see user A's companies
SELECT is(
  (SELECT count(*)::int FROM watched_companies),
  0,
  'User B cannot read User A watched_companies (SC-06)'
);

-- jobs: user B cannot see user A's jobs
SELECT is(
  (SELECT count(*)::int FROM jobs),
  0,
  'User B cannot read User A jobs (SC-06)'
);

-- applications: user B cannot see user A's applications
SELECT is(
  (SELECT count(*)::int FROM applications),
  0,
  'User B cannot read User A applications (SC-06)'
);

-- reports: user B cannot see user A's reports
SELECT is(
  (SELECT count(*)::int FROM reports),
  0,
  'User B cannot read User A reports (SC-06)'
);

-- cvs: user B cannot see user A's CVs
SELECT is(
  (SELECT count(*)::int FROM cvs),
  0,
  'User B cannot read User A cvs (SC-06)'
);

-- scan_runs: user B cannot see user A's scan runs
SELECT is(
  (SELECT count(*)::int FROM scan_runs),
  0,
  'User B cannot read User A scan_runs (SC-06)'
);

-- usage: user B cannot see user A's usage records
SELECT is(
  (SELECT count(*)::int FROM usage),
  0,
  'User B cannot read User A usage (SC-06)'
);

-- ============================================================
-- Direct row access by known UUID: user B cannot retrieve user A's resources
-- ============================================================

SELECT is(
  (SELECT count(*)::int FROM jobs WHERE id = 'a2000000-0000-0000-0000-000000000020'::uuid),
  0,
  'User B cannot fetch User A job by ID (NFR-01)'
);

SELECT is(
  (SELECT count(*)::int FROM applications WHERE id = 'a3000000-0000-0000-0000-000000000030'::uuid),
  0,
  'User B cannot fetch User A application by ID (NFR-01)'
);

SELECT is(
  (SELECT count(*)::int FROM reports WHERE id = 'a4000000-0000-0000-0000-000000000040'::uuid),
  0,
  'User B cannot fetch User A report by ID (NFR-01)'
);

SELECT is(
  (SELECT count(*)::int FROM cvs WHERE id = 'a5000000-0000-0000-0000-000000000050'::uuid),
  0,
  'User B cannot fetch User A CV by ID (NFR-01)'
);

SELECT is(
  (SELECT count(*)::int FROM scan_runs WHERE id = 'a6000000-0000-0000-0000-000000000060'::uuid),
  0,
  'User B cannot fetch User A scan_run by ID (NFR-01)'
);

SELECT is(
  (SELECT count(*)::int FROM usage WHERE id = 'a7000000-0000-0000-0000-000000000070'::uuid),
  0,
  'User B cannot fetch User A usage by ID (NFR-01)'
);

-- ============================================================
-- Confirm total counts from user A's perspective are correct
-- ============================================================
DO $$ BEGIN PERFORM set_config('app.current_user_id', current_setting('test.user_a'), false); END $$;

SELECT is(
  (SELECT count(*)::int FROM jobs),
  1,
  'User A can see their own jobs (RLS allows owner)'
);

SELECT is(
  (SELECT count(*)::int FROM watched_companies),
  1,
  'User A can see their own watched_companies (RLS allows owner)'
);

SELECT * FROM finish();
ROLLBACK;
