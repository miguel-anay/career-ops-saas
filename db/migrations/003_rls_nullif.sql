-- Migration 003: NULLIF GUC hardening (rls-tenancy-wiring, Req 4)
--
-- Hardens every tenant policy so an EMPTY-STRING app.current_user_id (left on
-- a pooled physical connection after a prior tenant tx ended — set_config
-- with is_local=true reverts the GUC to '' once that transaction commits or
-- rolls back) degrades to a clean DENY instead of casting ''::uuid into a
-- `22P02 invalid input syntax for type uuid` error (a 500 at the API layer).
-- NULL (GUC never set on this connection at all) already denied cleanly via
-- current_setting(..., true); this extends the same clean-deny behavior to
-- the empty-string case.
--
-- Empirically verified against the live DB as app_user (Seam 0 spike, D9):
--   - GUC unset            -> current_setting(..., true) is NULL -> clean deny, 0 rows, no error.
--   - GUC = '' (pooled reset value) -> ''::uuid -> ERROR 22P02 invalid input syntax for type uuid: "".
--   - GUC = valid uuid     -> clean scoping (no change after this migration).
-- This migration closes the '' gap by wrapping current_setting(...) in
-- NULLIF(..., '') before the ::uuid cast, so '' becomes NULL and the
-- equality comparison evaluates to NULL (excluded from USING/WITH CHECK),
-- not an error.
--
-- PG16 has no single-statement ALTER POLICY that rewrites both USING and
-- WITH CHECK in place, so every policy is DROP + CREATE (idempotent,
-- explicit, matches how 001_initial.sql / 002_ingest_cv.sql define them).
--
-- No change to ENABLE / FORCE ROW LEVEL SECURITY flags — they stay exactly
-- as set in 001_initial.sql / 002_ingest_cv.sql.

-- ---------------------------------------------------------------------------
-- tenant_users (keyed on id)
-- ---------------------------------------------------------------------------

DROP POLICY tenant_users ON users;
CREATE POLICY tenant_users ON users
  USING      (id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);

-- ---------------------------------------------------------------------------
-- The remaining 8 policies are keyed on user_id
-- ---------------------------------------------------------------------------

DROP POLICY tenant_watched_companies ON watched_companies;
CREATE POLICY tenant_watched_companies ON watched_companies
  USING      (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);

DROP POLICY tenant_jobs ON jobs;
CREATE POLICY tenant_jobs ON jobs
  USING      (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);

DROP POLICY tenant_applications ON applications;
CREATE POLICY tenant_applications ON applications
  USING      (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);

DROP POLICY tenant_reports ON reports;
CREATE POLICY tenant_reports ON reports
  USING      (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);

DROP POLICY tenant_cvs ON cvs;
CREATE POLICY tenant_cvs ON cvs
  USING      (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);

DROP POLICY tenant_scan_runs ON scan_runs;
CREATE POLICY tenant_scan_runs ON scan_runs
  USING      (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);

DROP POLICY tenant_usage ON usage;
CREATE POLICY tenant_usage ON usage
  USING      (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);

DROP POLICY tenant_cv_ingestions ON cv_ingestions;
CREATE POLICY tenant_cv_ingestions ON cv_ingestions
  USING      (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);
