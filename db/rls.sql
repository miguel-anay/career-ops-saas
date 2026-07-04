-- Row-Level Security for career-ops-saas
-- All policies use NULLIF(current_setting('app.current_user_id', true), '')::uuid
-- so an empty-string GUC (left on a pooled connection after a prior tenant
-- tx ended) degrades to a clean deny, not a 22P02 cast error
-- (db/migrations/003_rls_nullif.sql — rls-tenancy-wiring, Req 4).
-- The runtime DB role (app_user) MUST NOT be the table owner to avoid
-- implicit RLS bypass. Use FORCE ROW LEVEL SECURITY on app_user.

-- ---------------------------------------------------------------------------
-- Enable RLS on all user-scoped tables
-- ---------------------------------------------------------------------------

ALTER TABLE users             ENABLE ROW LEVEL SECURITY;
ALTER TABLE watched_companies ENABLE ROW LEVEL SECURITY;
ALTER TABLE jobs              ENABLE ROW LEVEL SECURITY;
ALTER TABLE applications      ENABLE ROW LEVEL SECURITY;
ALTER TABLE reports           ENABLE ROW LEVEL SECURITY;
ALTER TABLE cvs               ENABLE ROW LEVEL SECURITY;
ALTER TABLE scan_runs         ENABLE ROW LEVEL SECURITY;
ALTER TABLE usage             ENABLE ROW LEVEL SECURITY;
ALTER TABLE cv_ingestions     ENABLE ROW LEVEL SECURITY;
ALTER TABLE email_ingest_runs ENABLE ROW LEVEL SECURITY;

-- Force RLS even for the table owner role (app_user)
ALTER TABLE users             FORCE ROW LEVEL SECURITY;
ALTER TABLE watched_companies FORCE ROW LEVEL SECURITY;
ALTER TABLE jobs              FORCE ROW LEVEL SECURITY;
ALTER TABLE applications      FORCE ROW LEVEL SECURITY;
ALTER TABLE reports           FORCE ROW LEVEL SECURITY;
ALTER TABLE cvs               FORCE ROW LEVEL SECURITY;
ALTER TABLE scan_runs         FORCE ROW LEVEL SECURITY;
ALTER TABLE usage             FORCE ROW LEVEL SECURITY;
ALTER TABLE cv_ingestions     FORCE ROW LEVEL SECURITY;
ALTER TABLE email_ingest_runs FORCE ROW LEVEL SECURITY;

-- ---------------------------------------------------------------------------
-- Tenant policies
-- ---------------------------------------------------------------------------

CREATE POLICY tenant_users ON users
  USING (id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);

CREATE POLICY tenant_watched_companies ON watched_companies
  USING (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);

CREATE POLICY tenant_jobs ON jobs
  USING (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);

CREATE POLICY tenant_applications ON applications
  USING (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);

CREATE POLICY tenant_reports ON reports
  USING (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);

CREATE POLICY tenant_cvs ON cvs
  USING (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);

CREATE POLICY tenant_scan_runs ON scan_runs
  USING (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);

CREATE POLICY tenant_usage ON usage
  USING (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);

CREATE POLICY tenant_cv_ingestions ON cv_ingestions
  USING (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);

CREATE POLICY tenant_email_ingest_runs ON email_ingest_runs
  USING (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);
