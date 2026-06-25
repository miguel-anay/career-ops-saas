# Proposal: Web Feature-Folder Colocation ("Screaming" Frontend)

## Intent

`web/app/page.tsx` (277 lines) and `web/app/companies/page.tsx` (253 lines) each mix local `interface` types, fetch logic, table rendering, forms/dialogs, and (page.tsx) WebSocket handling in one component. `web/hooks/{useScanProgress,useJobProgress}.ts` are global but each used by exactly one feature. `web/lib/api.ts` mixes the generic HTTP client with CV-ingest-specific exports (`postIngest`, `getIngestion`). None of this is layered wrong — there is no business logic to extract — it is simply not colocated by feature, so any change to one feature risks touching unrelated code and creates merge-conflict surface area. "Screaming architecture" for a frontend means colocation by feature, not hexagonal layers. This is purely a structure refactor: NO behavior, visual output, or API contract changes.

## Scope

### In Scope
- Introduce `web/features/<feature>/` (components, hooks, feature-specific API calls, types) for `jobs` (dashboard) and `companies`.
- Split `app/page.tsx` and `app/companies/page.tsx` into feature components under `features/jobs/` and `features/companies/`; route files become thin composers.
- Write characterization tests for `app/companies/page.tsx` and `app/jobs/[id]/page.tsx` BEFORE moving any of their code (currently zero coverage).
- PR2 (follow-up, same change): move `useScanProgress`/`useJobProgress` into their feature folders; split `lib/api.ts` so `postIngest`/`getIngestion`/`IngestRunResponse`/`IngestionStatus` move into the `cv` feature, keeping the generic client (`apiGet/Post/Patch/Delete`) global. Update every `vi.mock` relative path touched by the move in the same PR.

### Out of Scope
- No new features, no visual change, no API contract change, no backend change.
- `tracker/page.tsx` and `cv/ingest/page.tsx` splits — nice-to-have only; include if low-cost in PR2, otherwise defer as follow-up.
- `web/components/ui/*` (shadcn primitives) — stays global, no design-system overhaul.
- Generic API client and `web/lib/auth.ts` — stay global.

## Capabilities

### New Capabilities
None — pure structural refactor, no new user-facing behavior.

### Modified Capabilities
None — no spec-level requirement changes; existing behavior is preserved exactly.

## Approach

Two chained PRs (stacked-to-main):
- **PR1**: characterization tests for `companies/page.tsx` and `jobs/[id]/page.tsx` (today untested) → split `page.tsx` (dashboard) into `features/jobs/` and `companies/page.tsx` into `features/companies/`, leaving `app/<route>/page.tsx` as thin composers.
- **PR2**: move `useScanProgress`/`useJobProgress` into their feature folders; split `lib/api.ts` (CV-ingest exports move to `features/cv/`); update all relative-path mocks (`../../lib/api`, `../../hooks/useScanProgress`, etc.) in the same PR as each move.

Rationale for sequencing: PR1 fixes the two fattest, riskiest files first and closes the test-coverage gap before any code moves — the existing tests for `page.tsx`/`tracker`/`ingest-cv` already mock by relative path, so file moves must land together with mock-path updates, never split across PRs.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `web/app/page.tsx` | Modified | Becomes thin composer rendering `features/jobs/JobsDashboard` (or similar) |
| `web/app/companies/page.tsx` | Modified | Becomes thin composer rendering `features/companies/CompaniesView` |
| `web/features/jobs/**` | New | Dashboard components, types, fetch logic moved from `page.tsx` |
| `web/features/companies/**` | New | Table/dialog/catalog-search components moved from `companies/page.tsx` |
| `web/__tests__/app/companies.test.tsx` | New | Characterization test, written before the split (PR1) |
| `web/__tests__/app/jobs-detail.test.tsx` | New | Characterization test, written before any future split (PR1) |
| `web/hooks/useScanProgress.ts`, `useJobProgress.ts` | Modified (PR2) | Move into `features/jobs/` and `features/cv/` respectively |
| `web/lib/api.ts` | Modified (PR2) | `postIngest`/`getIngestion`/types move to `features/cv/api.ts`; generic client stays |
| `web/__tests__/**` | Modified | Mock paths (`vi.mock('../../lib/api')`, `vi.mock('../../hooks/useScanProgress')`, etc.) updated in the same PR as each move |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Moving files breaks `vi.mock` relative-path mocks silently (compile/mock failure, not a clean test failure) | High | Update every mock path in the same commit/PR as the move; run `make test-web` after each move, not just at the end |
| Splitting untested `companies/page.tsx` / `jobs/[id]/page.tsx` blind changes behavior with nothing to catch it | High | Characterization tests written and green BEFORE any code is moved (precondition, not concurrent work) |
| PR2 scope creep (tracker/cv/ingest splits) inflates review size past 400-line budget | Medium | Keep tracker/cv/ingest splits explicitly optional; cut them to a follow-up change if they push PR2 over budget |

## Rollback Plan

Each PR is a pure file/structure move with no behavior change — revert via `git revert` of the PR's merge commit. No data migration, no API versioning, no feature flag needed. If PR1 reveals characterization tests catching real divergence, fix forward before merging (do not move code around a known-broken oracle).

## Dependencies

- Strict TDD active (`make test-all`; web: `cd web && npm test -- --run`) — existing + new characterization tests are the safety net for this refactor.
- None external.

## Success Criteria

- [ ] `make test-all` green after PR1 and after PR2
- [ ] No visual or behavioral change: identical rendered output, identical API calls, identical routes/URLs
- [ ] `companies/page.tsx` and `jobs/[id]/page.tsx` have characterization test coverage before their code is touched
- [ ] Each feature's components/hooks/API calls/types are colocated under `web/features/<feature>/`
- [ ] `web/lib/api.ts` retains only the generic client; CV-ingest-specific exports live in `features/cv/`
- [ ] All `vi.mock` paths updated and passing in the same PR as each corresponding file move
