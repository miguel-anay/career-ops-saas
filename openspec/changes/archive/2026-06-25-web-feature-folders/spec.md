# Delta for Web Frontend Structure (web-feature-folders)

No existing `openspec/specs/web-frontend-structure/spec.md` — this is a behavior-preservation
spec, not a new-capability spec. It pins CURRENT behavior as the contract the refactor must not
break.

## ADDED Requirements

### Requirement: Route Output and API Calls Are Behavior-Identical Post-Refactor

The system MUST render identical output and issue identical API calls, for `/` (dashboard),
`/companies`, and `/jobs/[id]`, before and after the feature-folder colocation refactor. Route
paths and URLs MUST NOT change.

#### Scenario: Dashboard renders same elements and calls after split

- GIVEN `app/page.tsx` is refactored into a thin composer rendering a `features/jobs/` component
- WHEN the dashboard route (`/`) is rendered with the same mocked `apiGet`/`apiPost`/`useScanProgress`
- THEN it calls `apiGet('/api/jobs?page=1&limit=20')` on mount, renders the jobs table, "Add" form,
  "Scan Now" button, and "Tracker"/"Companies" links exactly as before
- AND submitting the Add Job form calls `apiPost('/api/jobs', { url })` with the same payload shape

#### Scenario: Companies page renders same elements and calls after split

- GIVEN `app/companies/page.tsx` is refactored into a thin composer rendering a
  `features/companies/` component
- WHEN the companies route (`/companies`) is rendered with the same mocked `apiGet`/`apiPost`/`apiDelete`
- THEN it calls `apiGet('/api/companies')` and `apiGet('/api/companies/catalog')` on mount, renders
  the watched-companies table and the catalog search input exactly as before
- AND confirming delete calls `apiDelete('/api/companies/{id}')` exactly as before

#### Scenario: Routes are unchanged

- GIVEN the refactor moves component code into `web/features/<feature>/`
- WHEN any of `/`, `/companies`, `/jobs/[id]` is navigated to
- THEN the URL path and the file under `web/app/` that owns that route are unchanged (only its
  internal content becomes a thin composer)

### Requirement: Characterization Tests Precede Code Movement for Untested Pages

The system MUST have passing characterization tests for `app/companies/page.tsx` and
`app/jobs/[id]/page.tsx` BEFORE any of their code is moved into `web/features/`. These tests pin
rendered elements, API calls made, and key user interactions as they exist today.

#### Scenario: Companies characterization test exists before the move

- GIVEN `app/companies/page.tsx` has zero test coverage today
- WHEN a characterization test is added at `web/__tests__/app/companies.test.tsx`
- THEN it asserts: `apiGet('/api/companies')` and `apiGet('/api/companies/catalog')` are called on
  mount; the watched-companies table renders rows from the mocked response; the catalog search
  filters by name; selecting a catalog entry calls `apiPost('/api/companies', { catalog_id })`;
  clicking "Remove" then confirming calls `apiDelete('/api/companies/{id}')`
- AND this test passes against the CURRENT (pre-move) `app/companies/page.tsx`

#### Scenario: Jobs detail characterization test exists before any future move

- GIVEN `app/jobs/[id]/page.tsx` has zero test coverage today
- WHEN a characterization test is added at `web/__tests__/app/jobs-detail.test.tsx`
- THEN it asserts the page's current rendered elements and the exact API call(s) it makes on
  mount, using the page's current behavior as the oracle
- AND this test passes against the CURRENT (pre-move) `app/jobs/[id]/page.tsx`

#### Scenario: Move is blocked until characterization tests are green

- GIVEN a characterization test for a page has not yet been written or is failing
- WHEN code from that page would be moved into `web/features/`
- THEN the move MUST NOT proceed until the characterization test exists and passes

### Requirement: Existing Web Tests Stay Green Through Every File Move

The system MUST keep all pre-existing web tests (`page.test.tsx`, `tracker.test.tsx`,
`ingest-cv.test.tsx`, `useJobProgress.test.tsx`, and any others) passing throughout the refactor.
Any test that mocks a module by relative path (`vi.mock('../../lib/api')`,
`vi.mock('../../hooks/useScanProgress')`, etc.) MUST have its mock path updated in the SAME PR/commit
as the corresponding file move — never split across PRs.

#### Scenario: Mock path updated atomically with file move

- GIVEN `page.test.tsx` mocks `vi.mock('../../lib/api')` and `vi.mock('../../hooks/useScanProgress')`
- WHEN `useScanProgress.ts` moves into `features/jobs/`
- THEN the same PR updates the mock path in `page.test.tsx` (and any other test mocking that hook)
  to the new location
- AND `cd web && npm test -- --run` passes immediately after that PR, with no intermediate broken state

#### Scenario: No test left mocking a stale path

- GIVEN any file move under this change (PR1 or PR2)
- WHEN the move lands
- THEN no test in `web/__tests__/**` references the old (pre-move) relative path in a `vi.mock` call

### Requirement: Feature Code Is Colocated, Global Code Stays Global

The system MUST colocate feature-specific components, hooks, feature API calls, and types under
`web/features/<feature>/`. Route files under `web/app/` MUST become thin composers that import from
`web/features/`. Code shared across features MUST remain global and MUST NOT be duplicated into
feature folders.

#### Scenario: Jobs dashboard code colocated

- GIVEN the dashboard split (PR1)
- WHEN `app/page.tsx` is refactored
- THEN its `Job`/`JobsResponse` types, fetch logic, and table/form rendering live under
  `web/features/jobs/`, and `app/page.tsx` only composes the feature component

#### Scenario: Companies code colocated

- GIVEN the companies split (PR1)
- WHEN `app/companies/page.tsx` is refactored
- THEN its `Company`/`CatalogCompany` types, fetch logic, table, catalog-search, and delete-dialog
  rendering live under `web/features/companies/`, and `app/companies/page.tsx` only composes the
  feature component

#### Scenario: Hooks and CV-ingest API move into their feature (PR2)

- GIVEN `useScanProgress.ts` is used only by the jobs dashboard and `useJobProgress.ts` is used only
  by the CV/job-progress flow
- WHEN PR2 lands
- THEN `useScanProgress.ts` moves into `web/features/jobs/`, `useJobProgress.ts` moves into its
  consuming feature folder, and `postIngest`/`getIngestion`/`IngestRunResponse`/`IngestionStatus`
  move from `web/lib/api.ts` into `web/features/cv/api.ts`

#### Scenario: Generic infrastructure stays global

- GIVEN `web/components/ui/*` (shadcn primitives), `apiGet`/`apiPost`/`apiPatch`/`apiDelete` plus
  token-refresh logic in `web/lib/api.ts`, and `web/lib/auth.ts`
- WHEN the refactor is applied (PR1 and PR2)
- THEN none of these are moved into any `web/features/<feature>/` folder — they remain importable
  by any feature

### Requirement: No New Behavior Is Introduced

The system MUST NOT change visual output, add features, change backend/API contracts, or change
routes as part of this refactor. Any divergence detected by a characterization or existing test is
a refactor bug, not an intended change, and MUST be fixed before merging.

#### Scenario: Visual output unchanged

- GIVEN the dashboard or companies page before the refactor
- WHEN the same page is rendered after the refactor with identical mocked data
- THEN the rendered DOM structure (text content, buttons, table columns, links) is unchanged

#### Scenario: No new API calls introduced

- GIVEN the set of API calls a page makes today (captured by characterization or existing tests)
- WHEN the page is refactored
- THEN the refactored page makes the exact same set of API calls with the exact same paths/payloads
  — no calls added, removed, or reordered

#### Scenario: A characterization test catches real divergence

- GIVEN a characterization test fails after a code move because the moved code behaves differently
  than the original
- WHEN this is discovered during PR1 or PR2
- THEN the divergence is fixed forward (matched to original behavior) BEFORE the move is merged —
  the move is never merged on top of a known-broken test

### Requirement: Full Web Test Suite Passes After Each PR

The system MUST keep `cd web && npm test -- --run` green at the end of PR1 and at the end of PR2.

#### Scenario: Web suite green after PR1

- GIVEN PR1 (characterization tests + dashboard/companies split) is complete
- WHEN `cd web && npm test -- --run` is run
- THEN all tests pass, including the new `companies.test.tsx` and `jobs-detail.test.tsx`

#### Scenario: Web suite green after PR2

- GIVEN PR2 (hooks move + `lib/api.ts` split) is complete
- WHEN `cd web && npm test -- --run` is run
- THEN all tests pass, including any tests whose mock paths were updated for the move
