# Design: Web Feature-Folder Colocation

## Technical Approach

Pure structure refactor — no behavior, visual, or API contract changes. Introduce `web/features/<feature>/` for `jobs`, `companies`, and (PR2-optional) `cv`. The `@/*` tsconfig alias maps to web root (`"@/*": ["./*"]`), so `@/features/jobs/JobsDashboard` resolves cleanly without alias changes. `app/<route>/page.tsx` files become thin composers (~10-15 lines): import a feature view component, render it. All fetch logic, local `interface`s, tables, forms, and dialogs move into `features/<name>/`.

PR1 (this design's primary scope): characterization tests for the two untested pages, then split `app/page.tsx` and `app/companies/page.tsx`. PR2: migrate `useScanProgress`/`useJobProgress` into feature folders, split `lib/api.ts`.

## Architecture Decisions

| Decision | Choice | Alternatives considered | Rationale |
|---|---|---|---|
| Folder shape | `features/<name>/{components/, types.ts, api.ts, hooks.ts}` flat files, `components/` as dir only when >1 component | Single flat file per feature; nested `domain/application/adapter` layers | No business logic exists to layer (per explore.md) — colocation only. Hexagonal layering inside `features/` would be cargo-culting the API's pattern onto a presentation layer with no rules to isolate. |
| Route file responsibility | Thin composer: auth redirect guard + render `<FeatureView />` | Keep auth/redirect logic in feature component | `isAuthenticated()` + `router.replace('/login')` is route-level concern (Next.js App Router convention), kept in `page.tsx` consistent with `jobs/[id]/page.tsx`'s existing pattern |
| Hooks placement | `useScanProgress` → `features/jobs/hooks.ts`; `useJobProgress` → `features/cv/hooks.ts` | Keep both in global `web/hooks/` | Each hook is used by exactly one feature today (confirmed in proposal) — global placement implies shared use that doesn't exist |
| `lib/api.ts` split | Generic client (`apiGet/Post/Patch/Delete`, token refresh) stays in `lib/api.ts`; `postIngest`/`getIngestion`/`IngestRunResponse`/`IngestionStatus` move to `features/cv/api.ts` | Move everything; keep everything global | Generic HTTP verbs have no feature owner — every feature calls them. CV-ingest functions are feature-specific business calls, not infra. |
| PR sequencing | Stacked-to-main, 2 PRs: PR1 = characterization tests + jobs/companies split. PR2 = hooks + lib/api.ts split (tracker/cv splits deferred unless cheap) | Single PR for everything | PR1 alone touches ~530 lines across 2 fat files + 2 new test files; bundling PR2 risks blowing the 400-line review budget |

## Data Flow (unchanged — structural only)

```
Before:  app/page.tsx (fetch + state + render + WS)  ──→  apiGet/apiPost, useScanProgress
After:   app/page.tsx (composer)
              └──→ features/jobs/JobsDashboard.tsx (fetch + state + render + WS)
                        └──→ apiGet/apiPost (lib/api.ts, unchanged)
                        └──→ useScanProgress (features/jobs/hooks.ts, PR2)
```

No new data paths. Same API endpoints, same WS routes, same render output.

## File Changes — PR1

| File | Action | Description |
|---|---|---|
| `web/__tests__/app/companies.test.tsx` | Create | Characterization test — written and green BEFORE any split |
| `web/__tests__/app/jobs-detail.test.tsx` | Create | Characterization test — written and green BEFORE any split |
| `web/features/jobs/types.ts` | Create | `Job`, `JobsResponse` interfaces moved from `app/page.tsx` |
| `web/features/jobs/JobsDashboard.tsx` | Create | All state/fetch/render/WS logic moved from `app/page.tsx` (uses existing `useScanProgress` from `@/hooks/useScanProgress` — not yet moved) |
| `web/app/page.tsx` | Modify | Reduced to thin composer rendering `<JobsDashboard />` |
| `web/features/companies/types.ts` | Create | `Company`, `CatalogCompany`, `CompaniesResponse`, `CatalogResponse` moved |
| `web/features/companies/CompaniesView.tsx` | Create | All state/fetch/render/dialog/catalog-search logic moved |
| `web/features/companies/components/DeleteCompanyDialog.tsx` | Create (optional split-out) | Confirm-delete dialog, if extracting improves readability; otherwise inline in `CompaniesView.tsx` |
| `web/app/companies/page.tsx` | Modify | Reduced to thin composer rendering `<CompaniesView />` |
| `web/__tests__/app/page.test.tsx` | Modify | Update `vi.mock` paths (see Testing Strategy) |

## File Changes — PR2 (hooks + lib/api.ts split)

| File | Action | Description |
|---|---|---|
| `web/features/jobs/hooks.ts` | Create | `useScanProgress` moved from `web/hooks/useScanProgress.ts` |
| `web/features/cv/hooks.ts` | Create | `useJobProgress` moved from `web/hooks/useJobProgress.ts` |
| `web/hooks/useScanProgress.ts`, `web/hooks/useJobProgress.ts` | Delete | Superseded by feature-colocated versions |
| `web/features/cv/api.ts` | Create | `postIngest`, `getIngestion`, `IngestRunResponse`, `IngestionStatus` moved from `lib/api.ts` |
| `web/lib/api.ts` | Modify | Retains only `apiGet/Post/Patch/Delete` + token-refresh internals |
| `web/features/jobs/JobsDashboard.tsx` | Modify | Import `useScanProgress` from `@/features/jobs/hooks` instead of `@/hooks/useScanProgress` |
| `web/app/cv/ingest/page.tsx` | Modify | Import `useJobProgress` from `@/features/cv/hooks`, `postIngest`/`getIngestion` from `@/features/cv/api` |
| `web/__tests__/app/page.test.tsx`, `ingest-cv.test.tsx` | Modify | Update `vi.mock` paths to new hook/api locations |
| `web/__tests__/hooks/useScanProgress.test.ts` → `web/__tests__/features/jobs/hooks.test.ts` | Move | Relocate alongside source per existing test/source pairing convention |
| `web/__tests__/hooks/useJobProgress.test.tsx` → `web/__tests__/features/cv/hooks.test.tsx` | Move | Same |
| `web/__tests__/lib/api.test.ts` | Modify (optional) | If `postIngest`/`getIngestion` tests exist here, split or relocate; current file only tests `apiGet` — no split needed unless ingest tests are added later |

**Deferred (only if PR2 has budget left):** `app/tracker/page.tsx` → `features/tracker/`, `app/cv/ingest/page.tsx` → `features/cv/IngestCVView.tsx`. Both are simpler/cheaper than jobs/companies (no WS in tracker; cv/ingest already isolated). Cut to a follow-up change if PR2 risks exceeding 400 lines.

## Interfaces / Contracts

Thin route + feature-view sketch (PR1, `web/app/page.tsx` after split):

```tsx
// web/app/page.tsx
'use client'

import { useEffect } from 'react'
import { useRouter } from 'next/navigation'
import { isAuthenticated } from '@/lib/auth'
import { JobsDashboard } from '@/features/jobs/JobsDashboard'

export default function DashboardPage() {
  const router = useRouter()

  useEffect(() => {
    if (!isAuthenticated()) router.replace('/login')
  }, [router])

  return <JobsDashboard />
}
```

```tsx
// web/features/jobs/JobsDashboard.tsx
'use client'

import { useEffect, useState, useCallback } from 'react'
// ...same imports as today's app/page.tsx, minus useRouter/isAuthenticated
import { apiGet, apiPost } from '@/lib/api'
import { useScanProgress } from '@/hooks/useScanProgress' // PR2: '@/features/jobs/hooks'
import type { Job, JobsResponse } from './types'

export function JobsDashboard() {
  // identical state/fetch/render/WS body from today's DashboardPage,
  // minus the auth-guard useEffect (now in the route composer)
}
```

`types.ts` exports `Job`/`JobsResponse` (jobs) and `Company`/`CatalogCompany`/`CompaniesResponse`/`CatalogResponse` (companies) verbatim from current inline interfaces — no field changes.

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Characterization (companies) | `web/__tests__/app/companies.test.tsx` — renders companies table, catalog search/add, delete-dialog confirm flow, calls `apiGet('/api/companies')`, `apiGet('/api/companies/catalog')`, `apiPost('/api/companies', ...)`, `apiDelete('/api/companies/:id')`; mirrors page.test.tsx structure (`vi.mock('../../lib/api')`, `vi.mock('../../lib/auth')`, `vi.mock('sonner')`, `vi.mock('next/navigation')`) | Written and GREEN against current `app/companies/page.tsx` BEFORE any split — precondition, not concurrent |
| Characterization (job detail) | `web/__tests__/app/jobs-detail.test.tsx` — renders job header/score/badges, evaluate/generate-CV/download-CV buttons, expandable report blocks; mocks `useParams` (jobId), `apiGet` for job/report/cv (report and cv calls reject with 404 in the "no report yet" case), `apiPost` for evaluate/generate-cv | Written and GREEN against current `app/jobs/[id]/page.tsx` BEFORE any split |
| Unit (post-split) | `web/__tests__/app/page.test.tsx` keeps asserting through `DashboardPage` (now a thin wrapper) — `vi.mock('../../lib/api')` and `vi.mock('../../hooks/useScanProgress')` stay valid in PR1 since neither moves until PR2 | Same test file, same mocks, only verifies post-split behavior is identical |
| Unit (PR2 hook/api moves) | Every existing `vi.mock` pointing at a moved file updates IN THE SAME PR/commit as the move | `page.test.tsx`: `vi.mock('../../hooks/useScanProgress')` → `vi.mock('../../features/jobs/hooks')`. `ingest-cv.test.tsx`: `vi.mock('../../lib/api')` (postIngest/getIngestion) → `vi.mock('../../features/cv/api')`; `vi.mock('../../hooks/useJobProgress')` → `vi.mock('../../features/cv/hooks')` |
| Regression | `make test-web` after each individual move, not only at PR end | Catches mock-path breaks immediately, per proposal Risk #1 |

**Existing test files requiring mock-path updates and exact current mocks:**

| Test file | Current `vi.mock` targets | New target (which PR) |
|---|---|---|
| `__tests__/app/page.test.tsx` | `../../lib/api`, `../../lib/auth`, `../../hooks/useScanProgress`, `next/navigation`, `sonner` | `../../lib/api` unchanged; `../../hooks/useScanProgress` → `../../features/jobs/hooks` (PR2) |
| `__tests__/app/tracker.test.tsx` | `../../lib/api`, `../../lib/auth`, `next/navigation`, `sonner` | Unchanged unless tracker split happens (deferred) |
| `__tests__/app/ingest-cv.test.tsx` | `../../lib/api` (postIngest/getIngestion), `../../lib/auth`, `../../hooks/useJobProgress`, `next/navigation`, `sonner` | `../../lib/api` → `../../features/cv/api` (PR2); `../../hooks/useJobProgress` → `../../features/cv/hooks` (PR2) |
| `__tests__/lib/api.test.ts` | none (imports real module via dynamic `import('../../lib/api')`) | unchanged — only tests `apiGet`, which stays in `lib/api.ts` |
| `__tests__/lib/auth.test.ts` | none (dynamic import) | unchanged — `lib/auth.ts` stays global |
| `__tests__/hooks/useScanProgress.test.ts` | none (dynamic import `'../../hooks/useScanProgress'`) | Move file to `__tests__/features/jobs/hooks.test.ts`, update import to `'../../../features/jobs/hooks'` (PR2) |
| `__tests__/hooks/useJobProgress.test.tsx` | none (dynamic import `'../../hooks/useJobProgress'`) | Move file to `__tests__/features/cv/hooks.test.tsx`, update import to `'../../../features/cv/hooks'` (PR2) |

Rule: file move + mock-path/import update happen in the same PR, same commit where practical. Never split a move from its corresponding test-path update across PRs.

## Migration / Rollout

No migration required. Each PR is a pure file/structure move, revertible via `git revert` of the merge commit. No feature flags, no DB changes, no API changes.

## Open Questions

None — proposal scope is fully concrete; `DeleteCompanyDialog` extraction (inline vs. separate file) is a tasks-phase implementation detail, not a blocking design decision.
