# Tasks: Web Feature-Folder Colocation

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | PR1: ~620-680 (2 new char. tests ~140 lines + 2 page splits ~590 lines moved/touched) / PR2: ~260-320 |
| 400-line budget risk | High (PR1 alone exceeds budget as a single PR) |
| Chained PRs recommended | Yes |
| Suggested split | PR1 (char. tests + jobs/companies split) -> PR2 (hooks + lib/api.ts split) |
| Delivery strategy | auto-chain |
| Chain strategy | stacked-to-main |

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High

PR1 itself (~620-680 lines) still exceeds the 400-line single-PR budget. Per auto-chain, apply proceeds with the next autonomous slice; PR1 SHOULD be further sliced into PR1a (characterization tests only, ~140-180 lines, pure addition, zero risk to existing behavior) and PR1b (jobs split) / PR1c (companies split) if the reviewer wants tighter diffs. Work units below reflect this 4-unit shape; treat PR1a+PR1b+PR1c as one logical "PR1" stack if the team prefers 2 PRs total, or as 3 stacked PRs if tighter review is wanted.

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | Characterization tests for companies + jobs/[id] (RED then GREEN against current code) | PR1a | Base: main. Pure addition, ~150-180 lines, zero behavior risk |
| 2 | Split app/page.tsx into features/jobs/ | PR1b | Base: PR1a (or main if squashed). ~280-320 lines moved/touched |
| 3 | Split app/companies/page.tsx into features/companies/ | PR1c | Base: PR1b. ~260-300 lines moved/touched |
| 4 | Move useScanProgress/useJobProgress + split lib/api.ts | PR2 | Base: PR1c (main once PR1 merged). ~260-320 lines |

## Phase 1: Characterization Tests (PR1a — precondition, TEST-FIRST)

- [x] T-188 Read web/app/companies/page.tsx and web/app/jobs/[id]/page.tsx fully to enumerate every rendered element, API call, and interaction (already done in design.md — confirm no drift before writing tests)
- [x] T-189 RED: write web/__tests__/app/companies.test.tsx asserting apiGet('/api/companies') + apiGet('/api/companies/catalog') on mount, watched-companies table rows, catalog search filter, apiPost('/api/companies', {catalog_id}) on select, apiDelete('/api/companies/{id}') on confirm — run against current pre-move page.tsx, must be GREEN (characterization, not new behavior)
- [x] T-190 RED: write web/__tests__/app/jobs-detail.test.tsx asserting apiGet job/report/cv calls (report/cv 404-tolerant), score badge, evaluate/generate-CV/download-CV buttons, expandable report blocks — run against current pre-move page.tsx, must be GREEN
- [x] T-191 Run `cd web && npm test -- --run`; confirm both new tests pass and no existing test regressed. Verification: full suite green — this is the gate before Phase 2 may start

## Phase 2: Jobs Dashboard Split (PR1b)

- [x] T-192 Create web/features/jobs/types.ts — move Job, JobsResponse interfaces verbatim from app/page.tsx
- [x] T-193 Create web/features/jobs/JobsDashboard.tsx — move all state/fetch/WS-handling/render logic from DashboardPage (minus the auth-guard redirect), import types from ./types and useScanProgress from @/hooks/useScanProgress (unchanged location until PR2)
- [x] T-194 Reduce web/app/page.tsx to thin composer: auth redirect useEffect + `return <JobsDashboard />`
- [x] T-195 Run `cd web && npm test -- --run`; verify web/__tests__/app/page.test.tsx still passes unmodified (no mock path changes needed — useScanProgress/lib/api paths unaffected in PR1)
- [x] T-196 Run `cd web && npm test -- --run` full suite again to confirm no cross-feature regression before moving to Phase 3

## Phase 3: Companies Split (PR1c)

- [x] T-197 Create web/features/companies/types.ts — move Company, CatalogCompany, CompaniesResponse, CatalogResponse interfaces verbatim from app/companies/page.tsx
- [x] T-198 Create web/features/companies/CompaniesView.tsx — move all state/fetch/table/catalog-search/delete-dialog logic from CompaniesPage (minus auth-guard redirect)
- [x] T-199 (Optional) Extract web/features/companies/components/DeleteCompanyDialog.tsx if CompaniesView.tsx exceeds ~150 lines; otherwise keep dialog inline — design.md marks this non-blocking (kept inline — CompaniesView.tsx is ~210 lines but the dialog itself is small; extraction deferred as genuinely optional)
- [x] T-200 Reduce web/app/companies/page.tsx to thin composer: auth redirect useEffect + `return <CompaniesView />`
- [x] T-201 Run `cd web && npm test -- --run`; verify web/__tests__/app/companies.test.tsx (written in Phase 1) still passes against the now-split code — this is the test that proves the move was behavior-preserving
- [x] T-202 Run full `cd web && npm test -- --run` suite; confirm PR1 exit criteria: all tests green, including companies.test.tsx and jobs-detail.test.tsx (per spec "Full Web Test Suite Passes After Each PR")

## Phase 4: Hooks Migration (PR2)

- [ ] T-203 Create web/features/jobs/hooks.ts — move useScanProgress (and ScanEvent type) verbatim from web/hooks/useScanProgress.ts; update web/features/jobs/JobsDashboard.tsx import to `./hooks`
- [ ] T-204 Create web/features/cv/hooks.ts — move useJobProgress (and JobProgressStatus/JobProgressPayload types) verbatim from web/hooks/useJobProgress.ts; update web/app/cv/ingest/page.tsx import to `@/features/cv/hooks`
- [ ] T-205 Delete web/hooks/useScanProgress.ts and web/hooks/useJobProgress.ts
- [ ] T-206 Move web/__tests__/hooks/useScanProgress.test.ts -> web/__tests__/features/jobs/hooks.test.ts; update its dynamic import path from '../../hooks/useScanProgress' to '../../../features/jobs/hooks'
- [ ] T-207 Move web/__tests__/hooks/useJobProgress.test.tsx -> web/__tests__/features/cv/hooks.test.tsx; update its dynamic import path from '../../hooks/useJobProgress' to '../../../features/cv/hooks'
- [ ] T-208 Update web/__tests__/app/page.test.tsx: change `vi.mock('../../hooks/useScanProgress')` to `vi.mock('../../features/jobs/hooks')` in the SAME commit as T-203 (per spec: mock path update is atomic with the move)
- [ ] T-209 Update web/__tests__/app/ingest-cv.test.tsx: change `vi.mock('../../hooks/useJobProgress')` to `vi.mock('../../features/cv/hooks')` in the SAME commit as T-204
- [ ] T-210 Run `cd web && npm test -- --run`; verify no test still references the old hooks/ paths (grep vi.mock + dynamic import for 'hooks/useScanProgress' and 'hooks/useJobProgress' returns zero matches in web/__tests__/**)

## Phase 5: lib/api.ts Split (PR2, continued)

- [ ] T-211 Create web/features/cv/api.ts — move postIngest, getIngestion, IngestRunResponse, IngestionStatus verbatim from web/lib/api.ts; import apiGet/apiPost from '@/lib/api'
- [ ] T-212 Remove postIngest/getIngestion/IngestRunResponse/IngestionStatus from web/lib/api.ts — keep only apiGet/apiPost/apiPatch/apiDelete + refreshTokens/request internals
- [ ] T-213 Update web/app/cv/ingest/page.tsx imports: postIngest/getIngestion/IngestRunResponse/IngestionStatus now from '@/features/cv/api' (apiGet/apiPost imports, if any direct usage remains, stay from '@/lib/api')
- [ ] T-214 Update web/__tests__/app/ingest-cv.test.tsx: change `vi.mock('../../lib/api')` postIngest/getIngestion mock targets to `vi.mock('../../features/cv/api')` in the SAME commit as T-211/T-212 — verify web/__tests__/lib/api.test.ts (tests only apiGet) is untouched and still passes
- [ ] T-215 Run full `cd web && npm test -- --run`; confirm PR2 exit criteria — all tests green, no stale vi.mock path remains anywhere in web/__tests__/** (per spec "No test left mocking a stale path")

## Phase 6: Deferred / Optional (only if PR2 budget allows — cut to follow-up otherwise)

- [ ] T-216 (OPTIONAL, deferrable) Split web/app/tracker/page.tsx into web/features/tracker/ — only if PR2 is still under budget after Phase 4-5; otherwise defer to a follow-up change
- [ ] T-217 (OPTIONAL, deferrable) Split web/app/cv/ingest/page.tsx into web/features/cv/IngestCVView.tsx — only if PR2 is still under budget; otherwise defer to a follow-up change

## Out of Scope (explicitly not touched by any task above)
- No visual or behavioral change to any route's rendered output
- No backend/API contract change — all paths/payloads identical
- web/components/ui/* (shadcn primitives) stays global
- web/lib/api.ts generic client (apiGet/Post/Patch/Delete + refresh) stays global
- web/lib/auth.ts stays global
- Route paths under web/app/ are unchanged — only internal content becomes thin composers
