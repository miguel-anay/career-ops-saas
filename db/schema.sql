-- career-ops-saas schema
-- PostgreSQL 16

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ---------------------------------------------------------------------------
-- Enums
-- ---------------------------------------------------------------------------

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

-- ---------------------------------------------------------------------------
-- Tables
-- ---------------------------------------------------------------------------

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
-- user_id is redundant (denormalized) to allow RLS to filter without a JOIN
CREATE TABLE applications (
  id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  job_id     uuid        NOT NULL UNIQUE REFERENCES jobs(id) ON DELETE CASCADE,
  score      double precision,
  status     app_status_t NOT NULL DEFAULT 'Evaluated',
  notes      text,
  pdf_path   text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_applications_user ON applications(user_id);

-- reports
-- user_id is redundant (denormalized) to allow RLS to filter without a JOIN
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

-- usage (metering-ready; billing deferred to MVP+1)
CREATE TABLE usage (
  id                uuid    PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id           uuid    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  month             char(7) NOT NULL,
  evaluations_count integer NOT NULL DEFAULT 0,
  pdfs_count        integer NOT NULL DEFAULT 0,
  ingestions_count  integer NOT NULL DEFAULT 0,
  UNIQUE (user_id, month)
);

-- cv_ingestions
CREATE TABLE cv_ingestions (
  id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status      text        NOT NULL DEFAULT 'running'
                            CHECK (status IN ('running','completed','failed')),
  started_at  timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz
);
CREATE INDEX idx_cv_ingestions_user ON cv_ingestions(user_id, started_at DESC);
