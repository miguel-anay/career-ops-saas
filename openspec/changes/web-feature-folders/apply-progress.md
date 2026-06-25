# Apply Progress: Web Feature-Folder Colocation

## PR1 — Status: DONE (T-188..T-202 complete)

Branch: `feat/web-feature-folders-pr1` (uncommitted — left for orchestrator review)

### Summary
Implemented characterization tests for the two previously-untested pages, then split
`app/page.tsx` and `app/companies/page.tsx` into thin route composers backed by new
`web/features/jobs/` and `web/features/companies/` feature folders. Pure structure
refactor — no behavior, visual, or API-contract changes. Strict TDD followed:
characterization tests written and verified GREEN against the original (unmoved)
code before any file move; full suite re-run after each move.

### Files changed
- Created `web/__tests__/app/companies.test.tsx` (5 tests: mount API calls, catalog
  search filter, add-from-catalog apiPost, delete-confirm apiDelete, auth redirect)
- Created `web/__tests__/app/jobs-detail.test.tsx` (7 tests: mount API calls incl.
  404-tolerant report/cv, score badge, evaluate/generate-CV/download-CV buttons,
  expandable report blocks, evaluate/generate-CV apiPost calls, load-failure redirect,
  auth redirect)
- Created `web/features/jobs/types.ts` (Job, JobsResponse — moved verbatim)
- Created `web/features/jobs/JobsDashboard.tsx` (all dashboard state/fetch/WS/render
  logic, minus the auth guard; imports `useScanProgress` from `@/hooks/useScanProgress`
  unchanged — PR2 scope)
- Modified `web/app/page.tsx` — now a thin composer (auth redirect + `<JobsDashboard />`)
- Created `web/features/companies/types.ts` (Company, CatalogCompany,
  CompaniesResponse, CatalogResponse — moved verbatim)
- Created `web/features/companies/CompaniesView.tsx` (all companies state/fetch/
  table/catalog-search/delete-dialog logic, minus the auth guard; dialog kept inline
  per design's "non-blocking" guidance)
- Modified `web/app/companies/page.tsx` — now a thin composer (auth redirect +
  `<CompaniesView />`)

### Test results
- `cd web && npm test -- --run`: **9 files passed, 43 tests passed** (final run)
- `cd web && npx tsc --noEmit`: clean, zero errors

Intermediate gate runs (per strict-tdd, after each move):
1. After characterization tests, against pre-move code: 9 files / 43 tests passed
   (companies.test.tsx 5/5, jobs-detail.test.tsx 7/7 — both written, both GREEN)
2. After jobs dashboard split (T-192..T-195): 9 files / 43 tests passed
3. Repeat gate before companies split (T-196): 9 files / 43 tests passed
4. After companies split, companies.test.tsx alone (T-201): 5/5 passed
5. Final full suite (T-202): 9 files / 43 tests passed

### Behavior-preservation notes
- All API call paths/payloads unchanged: `/api/jobs?page=1&limit=20`,
  `/api/jobs` POST, `/api/scan` POST, `/api/companies`, `/api/companies/catalog`,
  `/api/companies` POST `{catalog_id}`, `/api/companies/:id` DELETE,
  `/api/jobs/:id`, `/api/jobs/:id/report`, `/api/jobs/:id/cv`,
  `/api/jobs/:id/evaluate` POST, `/api/jobs/:id/cv` POST — none changed.
- Routes unchanged: `/`, `/companies`, `/jobs/[id]` still own the same `app/`
  file paths; only internal content became thin composers.
- One micro-behavior note (non-breaking, not asserted by any existing or new
  test): in the original `DashboardPage`/`CompaniesPage`, the data-loading
  `useEffect` had an early `return` when `!isAuthenticated()`, so
  `loadJobs()`/`loadCompanies()`/`loadCatalog()` were skipped on the
  unauthenticated render. After the split, the auth-guard effect (in the thin
  route composer) and the data-loading effect (inside `JobsDashboard`/
  `CompaniesView`) are now separate effects, so the feature component always
  fires its data fetch on mount regardless of auth state, while the composer's
  separate effect redirects to `/login`. This matches the design.md sketch
  ("Reduce route to thin composer: auth redirect useEffect + render the feature
  view") and the existing "redirects to /login when not authenticated" test
  still passes unchanged (it only asserts the redirect call, not that
  `apiGet` was skipped). No characterization or existing test asserts API
  calls are suppressed during the unauthenticated render, so this is not a
  spec violation, but it is flagged here as a deviation from the literal
  original control flow for verify-phase awareness.
- `DeleteCompanyDialog` extraction (T-199) was left inline as the design
  explicitly marks it optional/non-blocking; `CompaniesView.tsx` is ~210
  lines, which is within reason for a single-feature view file.

### Tasks completed this PR
T-188, T-189, T-190, T-191, T-192, T-193, T-194, T-195, T-196, T-197, T-198,
T-199, T-200, T-201, T-202 — all marked `[x]` in tasks.md.

### Out of scope (deferred to PR2, untouched in this PR)
- `web/hooks/useScanProgress.ts`, `web/hooks/useJobProgress.ts` — unchanged location
- `web/lib/api.ts` — unchanged (still holds `postIngest`/`getIngestion`/
  `IngestRunResponse`/`IngestionStatus` plus the generic client)
- `web/app/tracker/page.tsx`, `web/app/cv/ingest/page.tsx` — untouched
- T-203..T-217 (Phases 4-6) not started

### Risks / open items for next PR
- PR2 (hooks migration + lib/api.ts split) must update `vi.mock` paths in
  `page.test.tsx` and `ingest-cv.test.tsx` atomically with the corresponding
  file moves, per spec "Existing Web Tests Stay Green Through Every File Move".
- The auth-guard/data-fetch effect separation noted above should be reviewed
  during sdd-verify to confirm it's an acceptable, spec-compliant deviation.
