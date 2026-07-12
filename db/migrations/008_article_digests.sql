-- Migration 008: article-digest — per-project proof-point entries.
-- Mirrors cvs (db/schema.sql:124-133) minus is_master; digests are an additive
-- list, not a single-active record. RLS forced from day one (ADR-3).

CREATE TABLE article_digests (
  id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title      text        NOT NULL,
  content_md text        NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_article_digests_user ON article_digests(user_id, created_at DESC);

ALTER TABLE article_digests ENABLE ROW LEVEL SECURITY;
ALTER TABLE article_digests FORCE  ROW LEVEL SECURITY;

CREATE POLICY tenant_article_digests ON article_digests
  USING      (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON article_digests TO app_user;
