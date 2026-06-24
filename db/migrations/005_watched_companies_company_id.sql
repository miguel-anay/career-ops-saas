-- 005_watched_companies_company_id.sql
-- Link watched_companies to the global companies_catalog instead of relying
-- only on copied inline columns. company_id is NULLABLE: catalog picks point
-- at a catalog row (single source of truth for careers_url/provider), while
-- manual/custom companies keep company_id NULL and use their inline columns.
--
-- Added now, while watched_companies holds no real user data, because
-- backfilling this FK later (matching thousands of copied rows to the catalog
-- by the fragile careers_url natural key) is far more painful and risky.
--
-- ON DELETE SET NULL: removing a catalog entry degrades a watch to a custom
-- entry (keeps the user's inline snapshot) rather than deleting their watch.

ALTER TABLE watched_companies
  ADD COLUMN IF NOT EXISTS company_id uuid REFERENCES companies_catalog(id) ON DELETE SET NULL;

-- A user watches a given catalog company at most once. Manual entries
-- (company_id NULL) are exempt via the partial index predicate.
CREATE UNIQUE INDEX IF NOT EXISTS idx_watched_companies_user_company
  ON watched_companies(user_id, company_id)
  WHERE company_id IS NOT NULL;

-- Fan-out access path for future shared scanning: "who watches catalog X".
CREATE INDEX IF NOT EXISTS idx_watched_companies_company
  ON watched_companies(company_id);
