# Verify Report — web-feature-folders

**Verdict: PASS** (0 CRITICAL, 0 WARNING, 1 SUGGESTION)

## Scope verified
PR1 (#26, merge 62e83af) + PR2 (#27, merge b90b436) — both merged to main. Tasks T-188..T-215 claimed complete; T-216/T-217 (tracker, cv-ingest splits) explicitly deferred/out of scope.

## Completeness — tasks vs code

| Task range | Claim | Verified |
|---|---|---|
| T-188..T-191 (characterization tests) | companies.test.tsx + jobs-detail.test.tsx added, pre-move pages green | CONFIRMED — both files exist, pass standalone (14/14 tests) |
| T-192..T-196 (jobs split) | features/jobs/{types.ts,JobsDashboard.tsx}, page.tsx thin composer | CONFIRMED — web/app/page.tsx is 18 lines (auth-redirect + `<JobsDashboard/>`) |
| T-197..T-202 (companies split) | features/companies/{types.ts,CompaniesView.tsx}, page.tsx thin composer | CONFIRMED — web/app/companies/page.tsx is 18 lines, same pattern |
| T-203..T-210 (hooks migration) | web/hooks/ deleted, hooks.ts moved into features/jobs and features/cv, test files moved + mock paths updated atomically | CONFIRMED — `web/hooks/` does not exist; `git show --stat` on PR2 merge shows clean renames (`useScanProgress.ts => features/jobs/hooks.ts`, `useJobProgress.ts => features/cv/hooks.ts`) with zero separate-commit drift |
| T-211..T-215 (lib/api.ts split) | postIngest/getIngestion/IngestRunResponse/IngestionStatus moved to features/cv/api.ts; lib/api.ts left with only generic client | CONFIRMED — `web/lib/api.ts` contains only `request`/`refreshTokens`/`apiGet`/`apiPost`/`apiPatch`/`apiDelete`; zero CV-specific symbols remain (grep confirms) |
| T-216/T-217 | Deferred | CONFIRMED NOT STARTED — `web/features/tracker/` and `web/features/cv/IngestCVView.tsx` do not exist; `web/app/jobs/[id]/page.tsx` untouched by `git diff --stat` |

## Behavior preservation (CORE) — auth-guard/data-fetch coupling

This was the single highest-risk point in the spec: the original pages used one `useEffect` combining the auth check and the redirect+fetch decision, so an unauthenticated render must never call the API.

**Verified preserved correctly in both files:**

- `web/features/jobs/JobsDashboard.tsx:58-64` — `useEffect` contains `if (!isAuthenticated()) return` before calling `loadJobs()`, with an inline comment: *"Preserve original behavior: no data fetch when unauthenticated (the route composer handles the redirect). Decoupling these would fire an API call that 401s before the redirect lands."*
- `web/features/companies/CompaniesView.tsx:59-65` — identical pattern: `if (!isAuthenticated()) return` before `loadCompanies()` + `loadCatalog()`.
- Route composers (`web/app/page.tsx`, `web/app/companies/page.tsx`) own only the redirect side-effect (`router.replace('/login')`), the feature component owns the guarded fetch — this is a clean split of one coupled effect into two effects that both gate on `isAuthenticated()` independently, preserving net behavior.

**Test coverage of the guard confirmed:**
- `web/__tests__/app/page.test.tsx:113` — `expect(mockApiGet).not.toHaveBeenCalled()` after `mockIsAuthenticated.mockReturnValue(false)`.
- `web/__tests__/app/companies.test.tsx:79` — same assertion pattern.
- Both tests pass in the live run.

No stale relative paths: `rg "hooks/useScanProgress|hooks/useJobProgress"` across `web/__tests__`, `web/features`, `web/app`, `web/lib` → zero matches. All `vi.mock(...)` calls in the 7 test files that mock app-local modules point to current paths (`../../features/jobs/hooks`, `../../features/cv/api`, `../../features/cv/hooks`, `../../lib/api`, `../../lib/auth`).

## No new behavior / no visual change

- `git show --stat` on both merge commits (62e83af, b90b436) shows the only files touched are: openspec artifacts, `web/__tests__/**`, `web/app/{page.tsx,companies/page.tsx,cv/ingest/page.tsx}`, `web/features/**`, `web/lib/api.ts`.
- Zero changes to `web/components/ui/*` or `web/lib/auth.ts`.
- Zero changes to any `api/` (Go backend) file from this change (the unrelated diff between `da6d47e` and `HEAD` for `api/` comes from other already-merged PRs, not this change's commits).
- API call paths/payloads unchanged — same endpoints (`/api/jobs`, `/api/companies`, `/api/companies/catalog`, `/api/companies/{id}`) verified via passing characterization + pre-existing tests.

## Live re-run evidence

```
cd web && npm test -- --run
 Test Files  9 passed (9)
      Tests  44 passed (44)
   Duration  4.61s

cd web && npx tsc --noEmit
(clean, zero output, exit 0)

cd api && go build ./...
(clean, zero output, exit 0)
```

Matches apply-progress's self-reported "9 files, 44 tests, all green" exactly — no drift between claim and live execution.

## Issues

**CRITICAL**: none.

**WARNING**: none.

**SUGGESTION**:
- `web/features/companies/CompaniesView.tsx` is ~226 lines (T-199 considered extracting `DeleteCompanyDialog.tsx` if it exceeded ~150 lines but kept it inline, noting "design.md marks this non-blocking"). Not a defect — explicitly called out and accepted in apply-progress — but flagging for awareness if this file grows further in future work.

## Final verdict

**PASS.** All 28 in-scope tasks (T-188..T-215) are complete and match the code on disk. The behavior-preservation contract — especially the auth-guard/no-fetch-when-unauthenticated invariant — is correctly preserved with both source comments and regression-test coverage. No stale mock paths. No scope creep into `components/ui`, `lib/auth.ts`, or the Go API. Live test/build/typecheck runs are all green. Safe to proceed to `sdd-archive`.
