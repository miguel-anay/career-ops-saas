-- Migration 007: Profile persistence (profile-persistence PR-A)
-- Adds users.profile_overrides (manual, top-level-key overrides that
-- survive CV re-ingestion) and profile_edits (RLS-forced ledger of every
-- manual/AI-suggested edit, generic enough for future source/status
-- values without a schema change).

-- ---------------------------------------------------------------------------
-- 1. Manual override column on users (users already has FORCE ROW LEVEL
--    SECURITY -> no rls.sql change needed for this column)
-- ---------------------------------------------------------------------------

ALTER TABLE users ADD COLUMN profile_overrides jsonb NOT NULL DEFAULT '{}'::jsonb;

-- ---------------------------------------------------------------------------
-- 2. profile_edits ledger
-- ---------------------------------------------------------------------------

CREATE TABLE profile_edits (
  id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  field_path  text        NOT NULL,          -- top-level key for this slice (e.g. "salary_target")
  old_value   jsonb,                          -- effective value BEFORE this edit (null if none)
  new_value   jsonb,                          -- value written into profile_overrides
  source      text        NOT NULL DEFAULT 'manual'
                            CHECK (source IN ('manual','ai_suggestion')),
  status      text        NOT NULL DEFAULT 'accepted'
                            CHECK (status IN ('accepted','proposed','undone')),
  created_at  timestamptz NOT NULL DEFAULT now(),
  resolved_at timestamptz
);
CREATE INDEX idx_profile_edits_user ON profile_edits(user_id, created_at DESC);

-- ---------------------------------------------------------------------------
-- 3. Row-Level Security (NULLIF-hardened per 003_rls_nullif.sql convention)
-- ---------------------------------------------------------------------------

ALTER TABLE profile_edits ENABLE ROW LEVEL SECURITY;
ALTER TABLE profile_edits FORCE  ROW LEVEL SECURITY;

CREATE POLICY tenant_profile_edits ON profile_edits
  USING      (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON profile_edits TO app_user;
