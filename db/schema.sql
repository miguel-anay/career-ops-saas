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
  id                  uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  email               text        NOT NULL UNIQUE,
  google_id           text        NOT NULL UNIQUE,
  plan                plan_t      NOT NULL DEFAULT 'free',
  cv_markdown         text,
  profile_json        jsonb       NOT NULL DEFAULT '{}'::jsonb,
  created_at          timestamptz NOT NULL DEFAULT now(),
  -- Gmail incremental-consent refresh token (gmail-job-ingestion). Nullable:
  -- existing users have none until they connect Gmail.
  google_refresh_token text
);

-- companies_catalog: global, install-wide reference list of known companies.
-- This is REFERENCE data, not tenant data: no user_id, no RLS. Users pick
-- from it to populate their own watched_companies (per-tenant, RLS-protected).
-- Stored once for the whole installation — not duplicated per user.
CREATE TABLE companies_catalog (
  id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  name        text        NOT NULL,
  careers_url text        NOT NULL UNIQUE,
  provider_id text        NOT NULL,
  ats_api_url text,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_companies_catalog_name ON companies_catalog(name);

-- watched_companies
CREATE TABLE watched_companies (
  id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name        text        NOT NULL,
  careers_url text,
  provider_id text,
  ats_api_url text,
  enabled     boolean     NOT NULL DEFAULT true,
  -- Link to the global catalog (NULL for manual/custom companies). The catalog
  -- is the single source of truth for careers_url/provider when set.
  company_id  uuid        REFERENCES companies_catalog(id) ON DELETE SET NULL,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_watched_companies_user ON watched_companies(user_id);
CREATE UNIQUE INDEX idx_watched_companies_user_company
  ON watched_companies(user_id, company_id)
  WHERE company_id IS NOT NULL;
CREATE INDEX idx_watched_companies_company ON watched_companies(company_id);

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
  status      text        NOT NULL DEFAULT 'pending'
                            CHECK (status IN ('pending','processing','completed','failed')),
  started_at  timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz
);
CREATE INDEX idx_cv_ingestions_user ON cv_ingestions(user_id, started_at DESC);

-- email_ingest_runs (gmail-job-ingestion): mirrors scan_runs minus
-- company_id-specific fields. Status vocabulary: running -> completed /
-- partial (some per-email errors) / error (no token / total failure).
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

-- article_digests (article-digest): per-project proof-point entries, an
-- additive list (no is_master, unlike cvs). Read at evaluation time as a
-- third cached prompt block (worker/lib/prompt.mjs).
CREATE TABLE article_digests (
  id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title      text        NOT NULL,
  content_md text        NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_article_digests_user ON article_digests(user_id, created_at DESC);
