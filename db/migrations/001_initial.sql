-- Migration 001: Initial schema
-- Runs schema + RLS + SECURITY DEFINER helper in order.
-- This file is mounted to /docker-entrypoint-initdb.d/ and executed once
-- when the postgres container is first initialised.

-- Create the runtime application role if it does not exist yet.
-- In production, create this role separately with a strong password.
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'app_user') THEN
    CREATE ROLE app_user WITH LOGIN PASSWORD 'app_pw';
  END IF;
END
$$;

-- Grant connect + usage on the database and public schema
GRANT CONNECT ON DATABASE careerops TO app_user;
GRANT USAGE ON SCHEMA public TO app_user;

-- ---------------------------------------------------------------------------
-- 1. Schema (enums + tables + indexes)
-- ---------------------------------------------------------------------------

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Enums

CREATE TYPE plan_t AS ENUM ('free', 'pro', 'unlimited');

CREATE TYPE job_status_t AS ENUM ('new', 'scanned', 'evaluated', 'archived');

CREATE TYPE app_status_t AS ENUM (
  'Evaluated',
  'Applied',
  'Responded',
  'Interview',
  'Offer',
  'Rejected',
  'Discarded',
  'SKIP'
);

CREATE TYPE scan_status_t AS ENUM ('running', 'completed', 'partial', 'failed');

-- users
CREATE TABLE users (
  id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  email        text        NOT NULL UNIQUE,
  google_id    text        NOT NULL UNIQUE,
  plan         plan_t      NOT NULL DEFAULT 'free',
  cv_markdown  text,
  profile_json jsonb       NOT NULL DEFAULT '{}'::jsonb,
  created_at   timestamptz NOT NULL DEFAULT now()
);

-- watched_companies
CREATE TABLE watched_companies (
  id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name        text        NOT NULL,
  careers_url text,
  provider_id text,
  ats_api_url text,
  enabled     boolean     NOT NULL DEFAULT true,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_watched_companies_user ON watched_companies(user_id);

-- jobs
CREATE TABLE jobs (
  id              uuid         PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         uuid         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title           text         NOT NULL,
  company         text         NOT NULL,
  url             text         NOT NULL,
  platform        text,
  status          job_status_t NOT NULL DEFAULT 'new',
  scraped_content text,
  evaluation_json jsonb,
  received_at     timestamptz,
  created_at      timestamptz  NOT NULL DEFAULT now(),
  UNIQUE (user_id, url)
);
CREATE INDEX idx_jobs_user_received ON jobs(user_id, received_at DESC);

-- applications
CREATE TABLE applications (
  id         uuid         PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    uuid         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  job_id     uuid         NOT NULL UNIQUE REFERENCES jobs(id) ON DELETE CASCADE,
  score      double precision,
  status     app_status_t NOT NULL DEFAULT 'Evaluated',
  notes      text,
  pdf_path   text,
  created_at timestamptz  NOT NULL DEFAULT now(),
  updated_at timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX idx_applications_user ON applications(user_id);

-- reports
CREATE TABLE reports (
  id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id        uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  application_id uuid        NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
  content_md     text        NOT NULL,
  blocks_json    jsonb       NOT NULL,
  created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_reports_application ON reports(application_id);

-- cvs
CREATE TABLE cvs (
  id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title      text        NOT NULL,
  content_md text        NOT NULL,
  is_master  boolean     NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_cvs_user ON cvs(user_id);
CREATE UNIQUE INDEX uq_cvs_master ON cvs(user_id) WHERE is_master;

-- scan_runs
CREATE TABLE scan_runs (
  id          uuid          PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     uuid          NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  started_at  timestamptz   NOT NULL DEFAULT now(),
  finished_at timestamptz,
  new_jobs    integer       NOT NULL DEFAULT 0,
  errors_json jsonb         NOT NULL DEFAULT '[]'::jsonb,
  status      scan_status_t NOT NULL DEFAULT 'running'
);
CREATE INDEX idx_scan_runs_user ON scan_runs(user_id, started_at DESC);

-- usage
CREATE TABLE usage (
  id                uuid    PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id           uuid    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  month             char(7) NOT NULL,
  evaluations_count integer NOT NULL DEFAULT 0,
  pdfs_count        integer NOT NULL DEFAULT 0,
  UNIQUE (user_id, month)
);

-- ---------------------------------------------------------------------------
-- 2. Row-Level Security
-- ---------------------------------------------------------------------------

ALTER TABLE users             ENABLE ROW LEVEL SECURITY;
ALTER TABLE watched_companies ENABLE ROW LEVEL SECURITY;
ALTER TABLE jobs              ENABLE ROW LEVEL SECURITY;
ALTER TABLE applications      ENABLE ROW LEVEL SECURITY;
ALTER TABLE reports           ENABLE ROW LEVEL SECURITY;
ALTER TABLE cvs               ENABLE ROW LEVEL SECURITY;
ALTER TABLE scan_runs         ENABLE ROW LEVEL SECURITY;
ALTER TABLE usage             ENABLE ROW LEVEL SECURITY;

ALTER TABLE users             FORCE ROW LEVEL SECURITY;
ALTER TABLE watched_companies FORCE ROW LEVEL SECURITY;
ALTER TABLE jobs              FORCE ROW LEVEL SECURITY;
ALTER TABLE applications      FORCE ROW LEVEL SECURITY;
ALTER TABLE reports           FORCE ROW LEVEL SECURITY;
ALTER TABLE cvs               FORCE ROW LEVEL SECURITY;
ALTER TABLE scan_runs         FORCE ROW LEVEL SECURITY;
ALTER TABLE usage             FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_users ON users
  USING (id = current_setting('app.current_user_id', true)::uuid)
  WITH CHECK (id = current_setting('app.current_user_id', true)::uuid);

CREATE POLICY tenant_watched_companies ON watched_companies
  USING (user_id = current_setting('app.current_user_id', true)::uuid)
  WITH CHECK (user_id = current_setting('app.current_user_id', true)::uuid);

CREATE POLICY tenant_jobs ON jobs
  USING (user_id = current_setting('app.current_user_id', true)::uuid)
  WITH CHECK (user_id = current_setting('app.current_user_id', true)::uuid);

CREATE POLICY tenant_applications ON applications
  USING (user_id = current_setting('app.current_user_id', true)::uuid)
  WITH CHECK (user_id = current_setting('app.current_user_id', true)::uuid);

CREATE POLICY tenant_reports ON reports
  USING (user_id = current_setting('app.current_user_id', true)::uuid)
  WITH CHECK (user_id = current_setting('app.current_user_id', true)::uuid);

CREATE POLICY tenant_cvs ON cvs
  USING (user_id = current_setting('app.current_user_id', true)::uuid)
  WITH CHECK (user_id = current_setting('app.current_user_id', true)::uuid);

CREATE POLICY tenant_scan_runs ON scan_runs
  USING (user_id = current_setting('app.current_user_id', true)::uuid)
  WITH CHECK (user_id = current_setting('app.current_user_id', true)::uuid);

CREATE POLICY tenant_usage ON usage
  USING (user_id = current_setting('app.current_user_id', true)::uuid)
  WITH CHECK (user_id = current_setting('app.current_user_id', true)::uuid);

-- ---------------------------------------------------------------------------
-- 3. SECURITY DEFINER helper for OAuth upsert (runs without tenant variable)
-- ---------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION auth_upsert_user(
  p_email     text,
  p_google_id text,
  p_name      text DEFAULT NULL
) RETURNS users AS $$
DECLARE
  v_user users;
BEGIN
  INSERT INTO users (email, google_id)
  VALUES (p_email, p_google_id)
  ON CONFLICT (google_id) DO UPDATE
    SET email = EXCLUDED.email
  RETURNING * INTO v_user;

  RETURN v_user;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER;

-- ---------------------------------------------------------------------------
-- 4. Grant DML to app_user on all tables
-- ---------------------------------------------------------------------------

GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO app_user;
GRANT EXECUTE ON FUNCTION auth_upsert_user(text, text, text) TO app_user;
