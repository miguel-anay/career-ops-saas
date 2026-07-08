# Apply Progress: evaluation-quality — PR-A (T-252..T-261)

Branch: `feat/48-evaluation-guards` (from `main`, up to date at start).
Status: PR-A complete, all in-scope tasks done, all tests green. Not pushed, no PR opened (orchestrator runs fresh review first).

## Task status

| Task | Status | Notes |
|------|--------|-------|
| T-252 | done | RED: `TestEnqueueEvaluation_CVMissing` + `TestEnqueueEvaluation_JobContentMissing` added to `api/internal/evaluate/service_test.go`. Deviation from tasks.md wording: implemented as DB-gated integration tests using the existing `rlsdb.Harness` (same pattern as `rls_integration_test.go` / `emailingest/service_test.go`), not a `testify/mock` unit test — `EnqueueEvaluation` calls `q.GetJobByID`/`q.GetUserByID` directly via sqlc `*db.Queries`, there's no injectable Servicer/Queries interface at that layer to mock without hitting a real DB. Handler-level mock tests (T-254) do use `testify/mock` as usual. |
| T-253 | done | `ErrCVMissing`, `ErrJobContentMissing` vars + `GetUserByID` call + guard order (CV then JD) added in `EnqueueEvaluation` (`api/internal/evaluate/service.go`), using `isBlank(sql.NullString)` helper (`!x.Valid \|\| strings.TrimSpace(x.String) == ""`). |
| T-254 | done | `Evaluate` handler switch (`api/internal/evaluate/handler.go`) maps `ErrCVMissing`→422 `cv_missing`, `ErrJobContentMissing`→422 `job_content_missing`. Added `TestEvaluate_CVMissing`/`TestEvaluate_JobContentMissing` mock-based handler tests for regression coverage (not explicitly in tasks.md but cheap and consistent with existing `TestEvaluate_UsageLimitExceeded` pattern). |
| T-255 | done | Full `api/internal/evaluate` suite green (mock handler tests + DB-gated integration tests), `ErrNotFound`/`ErrUsageLimitExceeded` precedence unchanged. Had to update `rls_integration_test.go`'s existing "owner EnqueueEvaluation still succeeds" subtest: seeded job now includes non-empty `scraped_content` and userA gets a seeded `cv_markdown`, otherwise the new guards correctly reject that fixture (it predates the guards). This is a fixture fix, not a behavior change. |
| T-256 | done | RED: `worker/tests/domain/evaluation-parser.characterization.test.mjs` updated — successful-parse fixtures now assert `evaluation.blocks` is an A→G array of `{label, content}` (oracle stays object-shaped, used only as a content/order source via `oracleBlocksToArray`); parse-error fixtures assert the sentinel object is unchanged but `Array.isArray(blocks) && blocks.length > 0` is false ("array-safe" per spec, not literally `[]` — see conflict note below). |
| T-257 | done | `EvaluationParser.parse` (`worker/domain/EvaluationParser.mjs`) now collects blocks by letter into `blocksByLetter`, then emits `BLOCK_LETTERS.filter(...).map(...)` as a fixed A→G array of `{label, content}`. Per-block `score` dropped (Open Question resolved: drop, YAGNI — nothing consumed it). Overall `score` (from `OVERALL_SCORE_PATTERN`) unaffected. `parseError` path unchanged. |
| T-258 | done | `worker/tests/adapters/pg-evaluation-repository.test.mjs` fixture changed from `{ blockA: {...} }` to `[{ label: 'Role Fit', content: 'Strong' }]`; the existing 5-`tenantQuery` assertions (upsert applications ON CONFLICT, DELETE stale reports, INSERT report, upsert usage, update jobs) were left untouched, confirmed already merged to main per the run instructions. |
| T-259 | done | Verified — `PgEvaluationRepository.save` needed zero code changes; `JSON.stringify(evaluation.blocks)` serializes the array as-is. |
| T-260 | done | `worker/lib/prompt.mjs`: added `received_at` to the job `SELECT`; new `describePostingAge(receivedAt)` helper returns `"posted N days ago"` (or `null` if missing/invalid, degrades silently — no crash for legacy jobs without `received_at`); injected as an optional `- **Posting age**: ...` line in `outputContract` (Block-G-adjacent data point, per design). `staticSystemPrompt` gained an "Additional guidance" section: STAR-mapping instruction, negotiation-guidance instruction, and a note to factor posting age into Block G's legitimacy assessment. Pure text — no provider branching, single `buildEvaluationPrompt` consumer confirmed (`worker/jobs/evaluate.mjs`). |
| T-261 | done | `worker/tests/lib/prompt.test.mjs`: 3 new cases — posting-age string present with frozen system time, STAR/negotiation guidance text present in system prompt, and a regression case asserting exactly 7 `## Block A-G` headers with `Score: X.X/5` / `Tier: 1-5` field names unchanged. |

## Commits (all on `feat/48-evaluation-guards`)

1. `c9de5c6` — `feat(api): reject evaluation with 422 when CV or job content is missing` (Go guards, T-252..T-255; 5 files, +188/-4).
2. `34758bb` — `fix(worker): emit evaluation blocks as an A-G array, not a keyed object` (parser flip, T-256..T-259; 4 files, +62/-15). Includes the necessary consequential fix to `worker/tests/application/evaluate-job.test.mjs` (see discovery below — not itself a listed task, but a direct regression from T-257).
3. `a652f96` — `feat(worker): add posting-age signal and STAR/negotiation guidance to prompt` (T-260..T-261; 2 files, +80/-3).

Total branch diff vs `main`: 11 files, +330/-22 (352 lines changed) — over the ~230-260 target in tasks.md's Review Workload Forecast, driven mostly by the new DB-gated Go integration tests (T-252) and the STAR/negotiation prompt test cases (T-261). Flagging as a size risk for the fresh reviewer; splitting further wasn't attempted since Unit 1 and Unit 2 are already independent commits and the whole slice is still one coherent, revertible PR-A.

## Test results

- `cd api && go test ./... -count=1` (no `TEST_DATABASE_URL`, DB-gated tests skip cleanly): all 14 testable packages pass, 0 failures.
- `cd api && TEST_DATABASE_URL=postgres://app_user:app_pw@localhost:5432/careerops?sslmode=disable go test ./... -count=1`: all packages pass, 146 `--- PASS` lines (including nested subtests), 0 failures. `internal/evaluate` alone: 15 top-level tests (13 mock/handler + 2 DB-gated integration test functions with subtests).
- `cd worker && npm test`: 30 test files, 171 tests passed, 4 skipped (pre-existing DB-gated integration tests unrelated to this change), 0 failures.

## Design conflicts / discoveries recorded (none blocking)

1. **spec.md vs design.md on parseError shape**: spec.md says parse failures should persist `blocks_json` as "e.g., an empty array"; design.md Decision 2 says `parseError` keeps its `{parse_error, raw}` object because the web guard (`Array.isArray(x) && x.length > 0`) is already falsy for a plain object. Followed design.md per the run contract ("design.md is the decided HOW — follow it"). Resolution: not a real conflict — "array-safe" only requires the guard not to throw/render, which a plain object already satisfies; implemented per design, added an explicit test asserting the guard is false for parse-error blocks.
2. **Task wording says "mock" for T-252, but `EnqueueEvaluation` has no injectable DB interface** — `Service` calls sqlc `*db.Queries` methods directly (no `Servicer`-style seam at the query layer). Implemented T-252 as DB-gated integration tests via the existing `rlsdb.Harness` (mirrors `rls_integration_test.go` / `emailingest/service_test.go` conventions already in the repo) instead of introducing a new mockable query interface, which would have been unrequested abstraction for one call site. Handler-level guard-code mapping (T-254) still uses `testify/mock` as the rest of the package does.
3. **`worker/jobs/pdf.mjs` reads `blocksJson?.blockA?.score`** to compute an average score badge for the generated CV PDF. This file is NOT in design.md's File Changes table and NOT in tasks.md T-252..T-266 — it was discovered via a full-repo grep for shape consumers before editing (root-cause check). After the array flip (T-257), new array-shaped reports will have `blocksJson.blockA` be `undefined`, so `overallScore` silently becomes `null` and the PDF's score badge stops rendering for newly-generated PDFs — no crash, no test failure (existing `pdf.test.mjs` fixtures are untouched and still use the legacy object shape, so they still pass), but a real, silent feature regression for anyone who downloads a CV PDF after a re-evaluation. Recommend a small follow-up: either read `applications.score` (already computed and stored) in `handleGeneratePDF` instead of averaging per-block scores, or keep a lightweight per-block score in the new array shape. Left untouched in this PR-A slice since it's out of the declared task/file scope and touching it would require a new DB read not sanctioned by design.md.

## Files changed

- `api/internal/evaluate/service.go`
- `api/internal/evaluate/handler.go`
- `api/internal/evaluate/service_test.go` (new)
- `api/internal/evaluate/handler_test.go`
- `api/internal/evaluate/rls_integration_test.go`
- `worker/domain/EvaluationParser.mjs`
- `worker/lib/prompt.mjs`
- `worker/tests/domain/evaluation-parser.characterization.test.mjs`
- `worker/tests/adapters/pg-evaluation-repository.test.mjs`
- `worker/tests/application/evaluate-job.test.mjs`
- `worker/tests/lib/prompt.test.mjs`

## Not touched (out of PR-A scope, confirmed)

- `web/lib/api.ts`, `web/app/jobs/[id]/page.tsx` — T-262..T-265 (PR-B).
- `openspec/changes/evaluation-quality/tasks.md` T-266 (cross-cutting `make test-all` — deferred to whoever runs the final chain step; ran the Go+worker equivalent here, web is PR-B's responsibility per the run contract).
