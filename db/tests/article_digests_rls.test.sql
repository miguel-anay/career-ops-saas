-- Run locally: make test-rls
-- Run manually (as app_user, the non-superuser runtime role, so FORCE RLS is
-- actually exercised — connecting as a superuser bypasses RLS unconditionally
-- regardless of FORCE ROW LEVEL SECURITY and would make this test a false
-- positive):
--   PGPASSWORD=app_pw pg_prove -U app_user -d careerops db/tests/article_digests_rls.test.sql
-- Requires: pgTAP extension + live PostgreSQL
--
-- pgTAP RLS isolation tests for article_digests (article-digest change,
-- spec.md "Row-level security enforces tenant isolation on article_digests"),
-- mirroring db/tests/cv_ingestions_rls.test.sql conventions.
--
-- Prerequisites:
--   1. pgTAP extension must be installed: CREATE EXTENSION pgtap;
--   2. Run as the app_user role (which has FORCE ROW LEVEL SECURITY applied
--      and does NOT bypass RLS, unlike the superuser role used to own the DB)
--   3. db/migrations/008_article_digests.sql (or the equivalent schema.sql +
--      rls.sql) must be applied

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
  v_user_a   users;
  v_user_b   users;
  v_digest_id uuid;
BEGIN
  SELECT * INTO v_user_a FROM auth_upsert_user('digest-itest-a@test.invalid', 'digest_itest_google_a', NULL);
  SELECT * INTO v_user_b FROM auth_upsert_user('digest-itest-b@test.invalid', 'digest_itest_google_b', NULL);
  PERFORM set_config('test.user_a', v_user_a.id::text, false);
  PERFORM set_config('test.user_b', v_user_b.id::text, false);

  -- Seed an article_digests row as user A
  PERFORM set_config('app.current_user_id', v_user_a.id::text, false);
  INSERT INTO article_digests (user_id, title, content_md)
  VALUES (v_user_a.id, 'Fraud detection pipeline', '## Hero metrics\nCut false positives 40%.')
  RETURNING id INTO v_digest_id;
  PERFORM set_config('test.digest_id', v_digest_id::text, false);
END;
$$;

-- ============================================================
-- Verify FORCE ROW LEVEL SECURITY is active on article_digests
-- ============================================================
SELECT ok(
  (SELECT relrowsecurity FROM pg_class WHERE relname = 'article_digests') = true,
  'RLS is enabled on article_digests table'
);

SELECT ok(
  (SELECT relforcerowsecurity FROM pg_class WHERE relname = 'article_digests') = true,
  'RLS is FORCED on article_digests table'
);

-- ============================================================
-- Cross-tenant isolation: switch to user B and verify no rows visible
-- ============================================================
SELECT set_config('app.current_user_id', current_setting('test.user_b'), false);

SELECT is(
  (SELECT count(*)::int FROM article_digests),
  0,
  'User B cannot read User A article_digests row'
);

SELECT is(
  (SELECT count(*)::int FROM article_digests WHERE id = current_setting('test.digest_id')::uuid),
  0,
  'User B cannot fetch User A article_digest by ID'
);

-- ============================================================
-- Write-path isolation: User B's DELETE against User A's row must
-- affect 0 rows (RLS hides the row from B's USING clause before the
-- DELETE can even match it) — User A's row remains afterward.
-- ============================================================
WITH deleted AS (
  DELETE FROM article_digests
  WHERE id = current_setting('test.digest_id')::uuid
  RETURNING id
)
SELECT is(
  (SELECT count(*)::int FROM deleted),
  0,
  'User B cannot DELETE User A article_digests row (write-path RLS)'
);

-- ============================================================
-- Owner path: switch back to user A and verify SELECT + DELETE succeed
-- normally, unaffected by the tenant policy.
-- ============================================================
SELECT set_config('app.current_user_id', current_setting('test.user_a'), false);

WITH deleted AS (
  DELETE FROM article_digests
  WHERE id = current_setting('test.digest_id')::uuid
  RETURNING id
)
SELECT is(
  (SELECT count(*)::int FROM deleted),
  1,
  'User A (owner) can SELECT/DELETE their own article_digests row'
);

SELECT * FROM finish();
ROLLBACK;
