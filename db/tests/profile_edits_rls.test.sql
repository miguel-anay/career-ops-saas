-- Run locally: make test-rls
-- Run manually (as app_user, the non-superuser runtime role, so FORCE RLS is
-- actually exercised — connecting as a superuser bypasses RLS unconditionally
-- regardless of FORCE ROW LEVEL SECURITY and would make this test a false
-- positive):
--   PGPASSWORD=app_pw pg_prove -U app_user -d careerops db/tests/profile_edits_rls.test.sql
-- Requires: pgTAP extension + live PostgreSQL
--
-- pgTAP RLS isolation tests for profile_edits (profile-persistence PR-A,
-- design D4). Validates that profile_edits forces RLS and a tenant policy
-- denies cross-tenant SELECT/UPDATE/INSERT, mirroring
-- db/tests/cv_ingestions_rls.test.sql conventions.
--
-- Prerequisites:
--   1. pgTAP extension must be installed: CREATE EXTENSION pgtap;
--   2. Run as the app_user role (which has FORCE ROW LEVEL SECURITY applied
--      and does NOT bypass RLS, unlike the superuser role used to own the DB)
--   3. db/migrations/007_profile_persistence.sql (or the equivalent
--      schema.sql/rls.sql) must be applied

BEGIN;
SELECT plan(6);

-- ============================================================
-- Setup: create two independent users via the SECURITY DEFINER helper
-- (auth_upsert_user bypasses RLS for setup exactly as it does in production
-- OAuth signup; this lets the test run as app_user end-to-end so FORCE RLS
-- is genuinely exercised by the assertions below, not silently bypassed by
-- a superuser connection)
-- ============================================================
DO $$
DECLARE
  v_user_a  users;
  v_user_b  users;
  v_edit_id uuid;
BEGIN
  SELECT * INTO v_user_a FROM auth_upsert_user('profile-edits-itest-a@test.invalid', 'profile_edits_itest_google_a', NULL);
  SELECT * INTO v_user_b FROM auth_upsert_user('profile-edits-itest-b@test.invalid', 'profile_edits_itest_google_b', NULL);
  PERFORM set_config('test.user_a', v_user_a.id::text, false);
  PERFORM set_config('test.user_b', v_user_b.id::text, false);

  -- Seed a profile_edits row as user A
  PERFORM set_config('app.current_user_id', v_user_a.id::text, false);
  INSERT INTO profile_edits (user_id, field_path, old_value, new_value)
  VALUES (v_user_a.id, 'narrative', NULL, '"Staff Engineer"'::jsonb)
  RETURNING id INTO v_edit_id;
  PERFORM set_config('test.edit_id', v_edit_id::text, false);
END;
$$;

-- ============================================================
-- Verify FORCE ROW LEVEL SECURITY is active on profile_edits
-- ============================================================
SELECT ok(
  (SELECT relrowsecurity FROM pg_class WHERE relname = 'profile_edits') = true,
  'RLS is enabled on profile_edits table'
);

SELECT ok(
  (SELECT relforcerowsecurity FROM pg_class WHERE relname = 'profile_edits') = true,
  'RLS is FORCED on profile_edits table'
);

-- ============================================================
-- Cross-tenant isolation: switch to user B and verify no rows visible
-- ============================================================
SELECT set_config('app.current_user_id', current_setting('test.user_b'), false);

SELECT is(
  (SELECT count(*)::int FROM profile_edits),
  0,
  'User B cannot read User A profile_edits row'
);

SELECT is(
  (SELECT count(*)::int FROM profile_edits WHERE id = current_setting('test.edit_id')::uuid),
  0,
  'User B cannot fetch User A profile_edits row by ID'
);

-- ============================================================
-- Write-path isolation: User B's UPDATE against User A's row must
-- affect 0 rows (RLS hides the row from B's USING clause before the
-- UPDATE can even match it) — proves the write path, not just SELECT.
-- ============================================================
WITH updated AS (
  UPDATE profile_edits
  SET status = 'undone', resolved_at = NOW()
  WHERE id = current_setting('test.edit_id')::uuid
  RETURNING id
)
SELECT is(
  (SELECT count(*)::int FROM updated),
  0,
  'User B cannot UPDATE User A profile_edits row (write-path RLS)'
);

-- ============================================================
-- Write-path isolation: User B cannot INSERT a row claiming User A's
-- id as user_id — the policy's WITH CHECK denies it even though B is
-- the one performing the INSERT.
-- ============================================================
SELECT throws_ok(
  format(
    $sql$INSERT INTO profile_edits (user_id, field_path) VALUES (%L, 'narrative')$sql$,
    current_setting('test.user_a')
  ),
  '42501',
  NULL,
  'User B cannot INSERT a profile_edits row for User A (WITH CHECK denies it)'
);

SELECT * FROM finish();
ROLLBACK;
