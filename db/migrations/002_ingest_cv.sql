-- Migration 002: Conversational CV ingestion (ingest-cv)
-- Adds cv_ingestions table (tenant-isolated, RLS-forced) and
-- usage.ingestions_count for per-month gating.

-- ---------------------------------------------------------------------------
-- 1. cv_ingestions table
-- ---------------------------------------------------------------------------

CREATE TABLE cv_ingestions (
  id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status      text        NOT NULL DEFAULT 'running'
                            CHECK (status IN ('running','completed','failed')),
  started_at  timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz
);
CREATE INDEX idx_cv_ingestions_user ON cv_ingestions(user_id, started_at DESC);

-- ---------------------------------------------------------------------------
-- 2. Row-Level Security
-- ---------------------------------------------------------------------------

ALTER TABLE cv_ingestions ENABLE ROW LEVEL SECURITY;
ALTER TABLE cv_ingestions FORCE  ROW LEVEL SECURITY;

CREATE POLICY tenant_cv_ingestions ON cv_ingestions
  USING       (user_id = current_setting('app.current_user_id', true)::uuid)
  WITH CHECK  (user_id = current_setting('app.current_user_id', true)::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON cv_ingestions TO app_user;

-- ---------------------------------------------------------------------------
-- 3. Usage accounting
-- ---------------------------------------------------------------------------

ALTER TABLE usage ADD COLUMN ingestions_count integer NOT NULL DEFAULT 0;
