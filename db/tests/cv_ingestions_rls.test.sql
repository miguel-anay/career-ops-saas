-- Run locally: make test-rls
-- Run manually (as app_user, the non-superuser runtime role, so FORCE RLS is
-- actually exercised — connecting as a superuser bypasses RLS unconditionally
-- regardless of FORCE ROW LEVEL SECURITY and would make this test a false
-- positive):
--   PGPASSWORD=app_pw pg_prove -U app_user -d careerops db/tests/cv_ingestions_rls.test.sql
-- Requires: pgTAP extension + live PostgreSQL
--
-- pgTAP RLS isolation tests for cv_ingestions (ingest-cv change, Req 6)
-- Validates that cv_ingestions forces RLS and a tenant policy denies
-- cross-tenant SELECT, mirroring db/tests/rls_test.sql conventions.
--
-- Prerequisites:
--   1. pgTAP extension must be installed: CREATE EXTENSION pgtap;
--   2. Run as the app_user role (which has FORCE ROW LEVEL SECURITY applied
--      and does NOT bypass RLS, unlike the superuser role used to own the DB)
--   3. The schema + rls.sql (or the equivalent migration) must be applied

BEGIN;
SELECT plan(4);

-- ============================================================
-- Setup: create two independent users via the SECURITY DEFINER helper
-- (auth_upsert_user bypasses RLS for setup exactly as it does in production
-- OAuth signup; this lets the test run as app_user end-to-end so FORCE RLS
-- is genuinely exercised by the assertions below, not silently bypassed by
-- a superuser connection)
-- ============================================================
DO $$
DECLARE
  v_user_a      users;
  v_user_b      users;
  v_ingestion_id uuid;
BEGIN
  SELECT * INTO v_user_a FROM auth_upsert_user('user_a@test.invalid', 'google_a_001', NULL);
  SELECT * INTO v_user_b FROM auth_upsert_user('user_b@test.invalid', 'google_b_002', NULL);
  PERFORM set_config('test.user_a', v_user_a.id::text, false);
  PERFORM set_config('test.user_b', v_user_b.id::text, false);

  -- Seed a cv_ingestions row as user A
  PERFORM set_config('app.current_user_id', v_user_a.id::text, false);
  INSERT INTO cv_ingestions (user_id, status)
  VALUES (v_user_a.id, 'running')
  RETURNING id INTO v_ingestion_id;
  PERFORM set_config('test.ingestion_id', v_ingestion_id::text, false);
END;
$$;

-- ============================================================
-- Verify FORCE ROW LEVEL SECURITY is active on cv_ingestions
-- ============================================================
SELECT ok(
  (SELECT relrowsecurity FROM pg_class WHERE relname = 'cv_ingestions') = true,
  'RLS is enabled on cv_ingestions table'
);

SELECT ok(
  (SELECT relforcerowsecurity FROM pg_class WHERE relname = 'cv_ingestions') = true,
  'RLS is FORCED on cv_ingestions table'
);

-- ============================================================
-- Cross-tenant isolation: switch to user B and verify no rows visible
-- ============================================================
SELECT set_config('app.current_user_id', current_setting('test.user_b'), false);

SELECT is(
  (SELECT count(*)::int FROM cv_ingestions),
  0,
  'User B cannot read User A cv_ingestions row (SC-06)'
);

SELECT is(
  (SELECT count(*)::int FROM cv_ingestions WHERE id = current_setting('test.ingestion_id')::uuid),
  0,
  'User B cannot fetch User A cv_ingestion by ID (NFR-01)'
);

SELECT * FROM finish();
ROLLBACK;
