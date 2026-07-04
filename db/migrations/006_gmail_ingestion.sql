-- Migration 006: Gmail job ingestion (gmail-job-ingestion PR 1)
-- Adds users.google_refresh_token (nullable — existing users have none until
-- they connect Gmail via the incremental consent flow) and email_ingest_runs
-- (tenant-isolated, RLS-forced, mirrors scan_runs minus company_id-specific
-- fields).

-- ---------------------------------------------------------------------------
-- 1. Refresh token on users (users already has FORCE ROW LEVEL SECURITY ->
--    no rls.sql change needed for this column)
-- ---------------------------------------------------------------------------

ALTER TABLE users ADD COLUMN google_refresh_token text;

-- ---------------------------------------------------------------------------
-- 2. email_ingest_runs table
-- ---------------------------------------------------------------------------

CREATE TABLE email_ingest_runs (
  id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status      text        NOT NULL DEFAULT 'running'
                            CHECK (status IN ('running','completed','partial','error')),
  new_jobs    integer     NOT NULL DEFAULT 0,
  errors_json jsonb       NOT NULL DEFAULT '[]'::jsonb,
  started_at  timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz
);
CREATE INDEX idx_email_ingest_runs_user ON email_ingest_runs(user_id, started_at DESC);

-- ---------------------------------------------------------------------------
-- 3. Row-Level Security (NULLIF-hardened per 003_rls_nullif.sql convention)
-- ---------------------------------------------------------------------------

ALTER TABLE email_ingest_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE email_ingest_runs FORCE  ROW LEVEL SECURITY;

CREATE POLICY tenant_email_ingest_runs ON email_ingest_runs
  USING      (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON email_ingest_runs TO app_user;
