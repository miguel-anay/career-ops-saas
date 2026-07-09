# Tasks: Evaluation Quality

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~430-480 (PR-A ~230-260, PR-B ~200-220) |
| 400-line budget risk | Medium |
| Chained PRs recommended | Yes |
| Suggested split | PR-A (Go guards + worker parser/prompt) → PR-B (web) |
| Delivery strategy | ask-on-risk |
| Chain strategy | pending |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: Medium

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | Go 422 content guards (CV + JD) | PR-A | Base = current branch/main; independent of unit 2 |
| 2 | Worker `blocks_json` array flip + prompt enrichment | PR-A | Depends on uncommitted re-eval upsert fix on `fix/42-pgboss-v10-batch` landing first (commit ordering, not code coupling) |
| 3 | Web 422 surfacing + array-render confirmation | PR-B | Base = PR-A branch (feature-branch-chain) or main (stacked); depends on PR-A's guard codes existing |

## Phase 1: Go Content Guards (Unit 1)

- [x] T-252 RED: add `TestEnqueueEvaluation_CVMissing` + `TestEnqueueEvaluation_JobContentMissing` to `api/internal/evaluate/service_test.go` asserting `ErrCVMissing`/`ErrJobContentMissing` and zero enqueue calls (mock). Est. 40 lines.
- [x] T-253 GREEN: add `ErrCVMissing`, `ErrJobContentMissing` vars + `GetUserByID` call + guard order (CV first, then JD, `!x.Valid || strings.TrimSpace(x.String) == ""`) in `EnqueueEvaluation` (`api/internal/evaluate/service.go`). Est. 30 lines.
- [x] T-254 Handler mapping: extend the `switch` in `Evaluate` (`api/internal/evaluate/handler.go:54`) with `case errors.Is(err, ErrCVMissing)` → 422 `cv_missing`, `case errors.Is(err, ErrJobContentMissing)` → 422 `job_content_missing`. Est. 6 lines.
- [x] T-255 Regression check: run `cd api && go test ./internal/evaluate/... -v` and confirm existing `ErrNotFound`/`ErrUsageLimitExceeded` precedence tests still pass unchanged (spec: "Existing error precedence is preserved"). No new lines — verification task.

**Acceptance (T-252..T-255)**: `POST /api/jobs/{id}/evaluate` returns 422 `cv_missing` when `users.cv_markdown` empty, 422 `job_content_missing` when `jobs.scraped_content` empty, no `evaluate-job` enqueued in either case; 404/402 paths unchanged.

## Phase 2: Worker Parser + Prompt (Unit 2)

- [x] T-256 RED: update `worker/tests/domain/evaluation-parser.characterization.test.mjs` assertions from keyed-object blocks (`blockA.title`) to array shape (`blocks[0].label`, A→G order); add a case for `parseError` → array-safe (`[]`) fallback. Est. 25 lines changed.
- [x] T-257 GREEN: in `EvaluationParser.parse` (`worker/domain/EvaluationParser.mjs:31-60`), collect blocks into the existing letter-keyed map (parsing unchanged), then emit `Object.entries` sorted A→G as `[{label, content}]` before calling `Evaluation.fromBlocks`. Keep `score` capture only if Open Question resolves to keep it — default: drop per YAGNI, note in commit body. Est. 15 lines.
- [x] T-258 RED: update `worker/tests/adapters/pg-evaluation-repository.test.mjs` fixtures/assertions expecting `blocks_json` as an object to expect an array (build on the already-uncommitted re-eval upsert fix on `fix/42-pgboss-v10-batch` — do not duplicate those 5 `tenantQuery` assertions, only the shape assertion changes). Est. 15 lines changed.
- [x] T-259 GREEN: confirm `PgEvaluationRepository.save`'s `JSON.stringify(evaluation.blocks)` needs no code change (array flows through as-is) — run `cd worker && npx vitest run tests/adapters/pg-evaluation-repository.test.mjs` to confirm GREEN. Verification-only, no new lines.
- [x] T-260 Prompt enrichment: add `received_at` to the job `SELECT` in `worker/lib/prompt.mjs:31`, compute posting age (`now - received_at`, human-readable days), inject as a Block-G data point, add STAR-mapping + negotiation-guidance sentences to `staticSystemPrompt`. Est. 20 lines.
- [x] T-261 Test: add/update a `worker/tests/lib/prompt*.test.mjs` (or nearest existing prompt test) case asserting the age string and STAR/negotiation text appear, and that block count/field names stay 7×A-G. Est. 20 lines.

**Acceptance (T-256..T-261)**: `cd worker && npm test` green; parser emits sorted A→G array; parseError path yields empty-array-safe blocks; prompt includes posting age + STAR/negotiation guidance; block schema unchanged.

**Dependency note**: T-256-259 build directly on the uncommitted `PgEvaluationRepository.mjs` re-evaluation fix already on `fix/42-pgboss-v10-batch`. Land/rebase that fix first; these tasks only touch the blocks-shape assertion, not the 5 `tenantQuery` DELETE-then-INSERT calls.

## Phase 3: Web 422 Surfacing (Unit 3 — PR-B)

- [x] T-262 Add `ApiError` class `{status, code}` to `web/lib/api.ts`; in `request()`, on `!response.ok` best-effort parse JSON body for `.code` and throw `ApiError` instead of generic `Error`. Est. 15 lines.
- [x] T-263 RED: add a `web/__tests__/jobs.test.tsx` (or existing job-detail test file) case simulating a 422 `cv_missing` response and asserting the CV-missing panel renders; a second case for `job_content_missing`. Est. 30 lines.
- [x] T-264 GREEN: in `handleEvaluate` (`web/app/jobs/[id]/page.tsx:107-117`), catch `ApiError`, set an `evalError: 'cv_missing' | 'job_content_missing' | null` state; render two distinct panels — cv_missing: "Add your CV via /cv/ingest" copy (issue #45 deep-link deferred); job_content_missing: "No readable job description yet" copy. Est. 35 lines.
- [x] T-265 Confirm `report.blocks_json && report.blocks_json.length > 0` guard (`page.tsx:214`) needs no change — it already treats blocks as an array; existing test "renders expandable report blocks" covers 7-block array; legacy object-shaped row safe because `Array.isArray` guard is falsy for objects. Est. 0 lines.

**Acceptance (T-262..T-265)**: `cd web && npm test -- --run` — 53 tests pass across 10 files; 422 `cv_missing`/`job_content_missing` render distinct actionable copy; array blocks render 7 sections (existing test); legacy object-shaped `blocks_json` degrades safely (guard is falsy for objects).

## Phase 4: Cross-cutting Verification

- [x] T-266 Run `make test-all`; confirm Go/worker/web suites pass together and no other caller of `lib/api.ts`'s generic `Error` broke (callers ignoring `.code` still work per design Decision 4).

## Dependencies Between Slices

- Unit 1 (Go guards) is independent of Unit 2 (worker) — no shared files, can be built/reviewed in parallel within PR-A.
- Unit 2 depends on the uncommitted `PgEvaluationRepository.mjs` re-eval fix (commit-order dependency only, land that first).
- Unit 3 (web) depends on Unit 1's error codes (`cv_missing`, `job_content_missing`) existing server-side and Unit 2's array shape existing for full end-to-end verification, but can be coded/reviewed against a mocked API — PR-B targets PR-A's branch (or main once PR-A merges), per chosen chain strategy.
