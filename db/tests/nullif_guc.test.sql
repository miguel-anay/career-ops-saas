-- Run locally: make test-rls
-- Run manually (as app_user, the non-superuser runtime role, so FORCE RLS is
-- actually exercised — connecting as a superuser bypasses RLS unconditionally
-- regardless of FORCE ROW LEVEL SECURITY and would make this test a false
-- positive):
--   PGPASSWORD=app_pw pg_prove -U app_user -d careerops db/tests/nullif_guc.test.sql
-- Requires: pgTAP extension + live PostgreSQL
--
-- pgTAP regression tests for the NULLIF GUC hardening (rls-tenancy-wiring,
-- Req 4). Proves that db/migrations/003_rls_nullif.sql changes the
-- empty-string-GUC failure mode from a 22P02 cast error to a clean RLS
-- denial (0 rows), without regressing the properly-set-GUC happy path.
--
-- Background: a pooled physical connection that ran a tenant tx (which sets
-- app.current_user_id via set_config(..., true), i.e. transaction-local)
-- reverts the GUC to '' (Postgres's default for a custom setting) once that
-- transaction ends. Before migration 003, the bare
-- current_setting('app.current_user_id', true)::uuid cast on '' raises
-- `22P02 invalid input syntax for type uuid`. After migration 003, every
-- policy uses NULLIF(current_setting(...), '')::uuid, so '' becomes NULL
-- and the comparison evaluates to NULL (cleanly excluded), not an error.
--
-- Prerequisites:
--   1. pgTAP extension must be installed: CREATE EXTENSION pgtap;
--   2. Run as the app_user role (which has FORCE ROW LEVEL SECURITY applied)
--   3. db/migrations/003_rls_nullif.sql (or the equivalent rls.sql) must be applied

BEGIN;
SELECT plan(4);

-- ============================================================
-- Setup: create a user + a jobs row via the SECURITY DEFINER helper
-- (auth_upsert_user bypasses RLS for setup exactly as it does in production
-- OAuth signup).
-- ============================================================
DO $$
DECLARE
  v_user_a users;
BEGIN
  SELECT * INTO v_user_a FROM auth_upsert_user('nullif-itest-a@test.invalid', 'nullif_itest_google_a', NULL);
  PERFORM set_config('test.user_a', v_user_a.id::text, false);

  PERFORM set_config('app.current_user_id', v_user_a.id::text, false);
  INSERT INTO jobs (user_id, title, company, url, platform, status)
  VALUES (v_user_a.id, 'Nullif Test Job', 'Acme Corp', 'https://boards.greenhouse.io/acme/jobs/nullif', 'greenhouse', 'new')
  ON CONFLICT DO NOTHING;
END;
$$;

-- ============================================================
-- Empty-string GUC: simulates a pooled connection whose prior tenant tx
-- ended (set_config(..., true) is transaction-local and reverts to the
-- session default — '' for an unset custom setting read with missing_ok).
-- After migration 003, this must deny cleanly: 0 rows, no 22P02.
-- ============================================================
SELECT set_config('app.current_user_id', '', false);

SELECT lives_ok(
  $sql$SELECT count(*) FROM jobs$sql$,
  'Empty-string app.current_user_id does not raise 22P02 against jobs (NULLIF hardening)'
);

SELECT is(
  (SELECT count(*)::int FROM jobs),
  0,
  'Empty-string app.current_user_id denies cleanly (0 rows), not an error'
);

-- ============================================================
-- Unset GUC (NULL, never set on this connection at all) must already have
-- denied cleanly before and after the migration — no regression.
-- ============================================================
SELECT set_config('app.current_user_id', NULL, false);

SELECT is(
  (SELECT count(*)::int FROM jobs),
  0,
  'Unset (NULL) app.current_user_id still denies cleanly after the NULLIF migration'
);

-- ============================================================
-- Happy path regression check: a properly-set GUC must still scope
-- correctly after the NULLIF migration (no behavior change for valid UUIDs).
-- ============================================================
SELECT set_config('app.current_user_id', current_setting('test.user_a'), false);

SELECT is(
  (SELECT count(*)::int FROM jobs WHERE user_id = current_setting('test.user_a')::uuid),
  1,
  'A properly-set GUC still returns the matching tenant row after the NULLIF migration'
);

SELECT * FROM finish();
ROLLBACK;
