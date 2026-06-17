-- Run locally: make test-rls
-- Run manually: pg_prove -U careerops -d careerops db/tests/cv_ingestions_rls.test.sql
-- Requires: pgTAP extension + live PostgreSQL
--
-- pgTAP RLS isolation tests for cv_ingestions (ingest-cv change, Req 6)
-- Validates that cv_ingestions forces RLS and a tenant policy denies
-- cross-tenant SELECT, mirroring db/tests/rls_test.sql conventions.
--
-- Prerequisites:
--   1. pgTAP extension must be installed: CREATE EXTENSION pgtap;
--   2. Run as the app_user role (which has FORCE ROW LEVEL SECURITY applied)
--   3. The schema + rls.sql (or the equivalent migration) must be applied

BEGIN;
SELECT plan(4);

-- ============================================================
-- Setup: create two independent users
-- ============================================================
DO $$
BEGIN
  INSERT INTO users (id, email, google_id, plan)
  VALUES
    ('a0000000-0000-0000-0000-000000000001'::uuid, 'user_a@test.invalid', 'google_a_001', 'free'),
    ('b0000000-0000-0000-0000-000000000002'::uuid, 'user_b@test.invalid', 'google_b_002', 'free')
  ON CONFLICT DO NOTHING;
END;
$$;

-- Seed a cv_ingestions row as user A
SET app.current_user_id = 'a0000000-0000-0000-0000-000000000001';

INSERT INTO cv_ingestions (id, user_id, status)
VALUES ('a8000000-0000-0000-0000-000000000080'::uuid, 'a0000000-0000-0000-0000-000000000001'::uuid, 'running')
ON CONFLICT DO NOTHING;

-- ============================================================
-- Verify FORCE ROW LEVEL SECURITY is active on cv_ingestions
-- ============================================================
SELECT ok(
  (SELECT rowsecurity FROM pg_class WHERE relname = 'cv_ingestions') = true,
  'RLS is enabled on cv_ingestions table'
);

SELECT ok(
  (SELECT relforcerowsecurity FROM pg_class WHERE relname = 'cv_ingestions') = true,
  'RLS is FORCED on cv_ingestions table'
);

-- ============================================================
-- Cross-tenant isolation: switch to user B and verify no rows visible
-- ============================================================
SET app.current_user_id = 'b0000000-0000-0000-0000-000000000002';

SELECT is(
  (SELECT count(*)::int FROM cv_ingestions),
  0,
  'User B cannot read User A cv_ingestions row (SC-06)'
);

SELECT is(
  (SELECT count(*)::int FROM cv_ingestions WHERE id = 'a8000000-0000-0000-0000-000000000080'::uuid),
  0,
  'User B cannot fetch User A cv_ingestion by ID (NFR-01)'
);

SELECT * FROM finish();
ROLLBACK;
