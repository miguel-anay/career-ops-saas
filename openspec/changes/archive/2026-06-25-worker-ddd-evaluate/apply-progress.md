# Apply Progress: DDD-lite Refactor of the Evaluate-Job Path

**Slices**: PR1 — Domain layer + characterization gate (T-157..T-169) | PR2 — Application + adapters + handler reduction + test rewrite + cleanup (T-170..T-187)
**Mode**: Strict TDD
**Branch**: feat/worker-ddd-evaluate-wiring (PR2; built on main, which already contains PR1's domain/ layer)
**Status**: 31/31 tasks complete (T-157..T-187). Both slices done. `make test-all` fully green.

## Completed Tasks (T-157..T-169) — PR1

- [x] T-157 — characterization test file created with inline oracle (verbatim `parseEvaluationResponse` copy) + 4 fixtures (full A-G+overall, partial blocks, empty, malformed)
- [x] T-158 — `worker/domain/EvaluationParser.mjs` created, zero imports from lib/db|anthropic|prompt
- [x] T-159 — characterization assertions: `EvaluationParser.parse` output structurally equals oracle for all 4 fixtures — GREEN
- [x] T-160 — `worker/tests/domain/score.test.mjs` written first (RED confirmed: Score.mjs did not exist)
- [x] T-161 — `worker/domain/Score.mjs` implemented: `Score.of(value)` validates [0,5] or null, throws RangeError/TypeError, NO isRecommended/RECOMMEND_THRESHOLD
- [x] T-162 — `worker/tests/domain/evaluation.test.mjs` written first (RED confirmed: Evaluation.mjs did not exist)
- [x] T-163 — `worker/domain/Evaluation.mjs` implemented: `fromBlocks`/`parseError` factories, no public constructor (T-58 unviolatable by construction — throws if blocks empty or contentMd not a string)
- [x] T-164 — `worker/tests/domain/no-mocks.test.mjs` guard: confirms domain/*.mjs import none of lib/db|anthropic|prompt, and tests/domain/*.test.mjs use no vi.mock
- [x] T-165 — `EvaluationParser.parse` returns `Evaluation.fromBlocks(...)`/`Evaluation.parseError(...)` instances (built directly to final form, not refactored twice)
- [x] T-166 — characterization test asserts empty string and garbled text both produce `isParseError === true` and never throw
- [x] T-167 — `EvaluationParser.parse` wraps regex body in try/catch returning `Evaluation.parseError(responseText)` on any internal exception
- [x] T-168 — `npx vitest run worker/tests/domain` → 4 files, 32 tests, 0 failures, 0 skipped
- [x] T-169 — confirmed: `git diff --stat worker/jobs/evaluate.mjs` shows no changes; domain/ exists in parallel, unused by production code

## Completed Tasks (T-170..T-187) — PR2

- [x] T-170 — `worker/adapters/_ports.js` created: JSDoc-only `EvaluatorPort {evaluate(userId, jobId): Promise<string>}` and `EvaluationRepository {save(userId, jobId, evaluation): Promise<void>}`, mirroring `worker/providers/_types.js` style (no runtime export besides `export {}`)
- [x] T-171 — `worker/tests/application/evaluate-job.test.mjs` written first (RED confirmed: EvaluateJob.mjs did not exist); covers happy path AND parse-error path with fake evaluator + spy repository
- [x] T-172 — `worker/application/EvaluateJob.mjs` implemented: constructor DI `new EvaluateJob({evaluator, repository})`, `.run({userId, jobId})` — `rawText = await evaluator.evaluate(userId, jobId); evaluation = EvaluationParser.parse(rawText); await repository.save(userId, jobId, evaluation)`. Zero regex, zero SQL.
- [x] T-173 — `worker/tests/adapters/anthropic-evaluator.test.mjs` written first (RED confirmed: AnthropicEvaluator.mjs did not exist); covers happy path + empty-content-blocks edge case
- [x] T-174 — `worker/adapters/AnthropicEvaluator.mjs` implemented: wraps `buildEvaluationPrompt` + `evaluate` (lib/prompt.mjs, lib/anthropic.mjs) behind `EvaluatorPort`, constructor `new AnthropicEvaluator({tenantQuery, buildEvaluationPrompt, evaluate})`
- [x] T-175 — `worker/tests/adapters/pg-evaluation-repository.test.mjs` written first (RED confirmed: PgEvaluationRepository.mjs did not exist); asserts EXACTLY 4 `tenantQuery` calls in order with correct SQL fragments and param values, for BOTH `fromBlocks` and `parseError` evaluations — inherits the T-58/4-write/order assertions from the old `evaluate.test.mjs`
- [x] T-176 — froze system time (`vi.setSystemTime(new Date('2026-06-25T12:00:00.000Z'))` in `beforeEach`, `vi.useRealTimers()` in `afterEach`) so the `YYYY-MM` usage-month assertion (`'2026-06'`) never flakes across a real month boundary
- [x] T-177 — `worker/adapters/PgEvaluationRepository.mjs` implemented: reproduces the 4 SQL statements byte-for-byte from the original `evaluate.mjs` — (1) INSERT applications (score, status 'Evaluated', notes=evaluation.statusNote), (2) INSERT reports (content_md, blocks_json), (3) UPSERT usage (evaluations_count+1, current YYYY-MM), (4) UPDATE jobs SET status='evaluated'
- [x] T-178 — `worker/tests/jobs/evaluate.test.mjs` rewritten: still mocks the 3 external boundaries the adapters wrap (`tenantQuery`, `buildEvaluationPrompt`, `evaluate` — same mock points as before, since the thin shim still imports them to construct real adapters), but assertions now target specific INSERT/SQL param shapes per scenario instead of a single blanket call-count check; the 4-write/order contract itself now lives in `tests/adapters/pg-evaluation-repository.test.mjs` as the source of truth. Used as an approval test first (ran green against the OLD unreduced handler) before reducing the handler — proves the rewrite doesn't silently change assertions to match new code.
- [x] T-179 — `worker/jobs/evaluate.mjs` reduced to a thin shim: constructs `AnthropicEvaluator` + `PgEvaluationRepository` + `EvaluateJob`, calls `.run({userId: user_id, jobId: job_id})`. Removed the inline `parseEvaluationResponse` function and all 4 inline `tenantQuery` calls (verified via `rg parseEvaluationResponse worker/jobs/evaluate.mjs` → zero matches).
- [x] T-180 — confirmed `worker/index.mjs` is byte-for-byte unchanged (`git diff --stat index.mjs` → empty) and `tests/index.test.mjs` still passes, proving the `handleEvaluateJob` import/registration surface is unchanged
- [x] T-181 — `make test-all` run: Go (all packages pass), worker (88 passed, 2 pre-existing skips, 0 failures), web (30 passed) — fully green
- [x] T-182 — grepped `domain/`, `application/`, `adapters/`, `jobs/evaluate.mjs` for `RECOMMEND_THRESHOLD`/`isRecommended` — only match is a comment in `Score.mjs` explicitly documenting their deliberate absence; zero behavioral matches
- [x] T-183 — confirmed `worker/domain/EvaluationParser.mjs` is the single source of parsing logic; `worker/jobs/evaluate.mjs` contains zero `parseEvaluationResponse` references after T-179
- [x] T-184 — confirmed `worker/tests/domain/evaluation-parser.characterization.test.mjs` still present, still green (12/12), still asserting against the original inlined oracle — kept as a permanent regression guard, not deleted
- [x] T-185 — confirmed `AnthropicEvaluator.evaluate()`'s calls (`buildEvaluationPrompt(userId, jobId, {tenantQuery})` then `evaluate(promptData.system, promptData.messages?.[0]?.content || '')`) match the original inline calls exactly; `worker/lib/anthropic.mjs` and `worker/lib/prompt.mjs` are git-diff-clean (zero changes) — `claude-sonnet-4-6`/`max_tokens: 8000`/`temperature: 0.2` untouched
- [x] T-186 — `worker/jobs/evaluate.mjs`'s file-level JSDoc comment rewritten to describe the new thin wiring-shim role, pointing to `application/EvaluateJob.mjs`, `domain/EvaluationParser.mjs`, and `adapters/PgEvaluationRepository.mjs`
- [x] T-187 — confirmed `worker/jobs/scan.mjs`, `worker/jobs/pdf.mjs`, `worker/jobs/ingest-cv.mjs` are git-diff-clean (zero changes)

## TDD Cycle Evidence

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|------|-----------|-------|------------|-----|-------|-------------|----------|
| T-157/T-159/T-166 (PR1) | `worker/tests/domain/evaluation-parser.characterization.test.mjs` | Unit | ✅ 3/3 (evaluate.test.mjs baseline before any change) | ✅ Written | ✅ Passed (12/12) | ✅ 4 fixtures × oracle-equality + 2 never-throw scenarios | ➖ None needed |
| T-160/T-161 (PR1) | `worker/tests/domain/score.test.mjs` | Unit | N/A (new file) | ✅ Written (failed: Score.mjs missing) | ✅ Passed (6/6) | ✅ 6 cases (bounds, null, type, no-threshold) | ➖ None needed |
| T-162/T-163 (PR1) | `worker/tests/domain/evaluation.test.mjs` | Unit | N/A (new file) | ✅ Written (failed: Evaluation.mjs missing) | ✅ Passed (7/7) | ✅ 7 cases (fromBlocks happy/null-score, parseError, T-58 construction guards) | ➖ None needed |
| T-164 (PR1) | `worker/tests/domain/no-mocks.test.mjs` | Unit (static guard) | N/A (new file) | ✅ Written | ✅ Passed (7/7, after excluding self from vi.mock string scan) | ➖ Single (structural check, not behavior) | ✅ Excluded self-file from scan after first run caught a false positive |
| T-171/T-172 (PR2) | `worker/tests/application/evaluate-job.test.mjs` | Unit | N/A (new file) | ✅ Written (failed: EvaluateJob.mjs missing) | ✅ Passed (2/2) | ✅ 2 cases (happy path full A-G+overall, parse-error garbled text) | ➖ None needed |
| T-173/T-174 (PR2) | `worker/tests/adapters/anthropic-evaluator.test.mjs` | Unit | N/A (new file) | ✅ Written (failed: AnthropicEvaluator.mjs missing) | ✅ Passed (2/2) | ✅ 2 cases (full response, empty content-blocks edge case) | ➖ None needed |
| T-175/T-176/T-177 (PR2) | `worker/tests/adapters/pg-evaluation-repository.test.mjs` | Unit | N/A (new file) | ✅ Written (failed: PgEvaluationRepository.mjs missing) | ✅ Passed (2/2) | ✅ 2 cases (fromBlocks happy path, parseError path) — frozen system time avoids month-boundary flake | ➖ None needed |
| T-178/T-179 (PR2) | `worker/tests/jobs/evaluate.test.mjs` (rewritten) | Unit (approval test) | ✅ 3/3 ran green against OLD unreduced handler BEFORE reducing it (approval testing protocol) | ✅ Rewritten first | ✅ Passed (3/3) after handler reduction — same external mock points, refined assertions | ✅ 3 scenarios (happy path, parse-error guard, empty-response parse_error shape) | ➖ None needed |
| T-180 (PR2) | `worker/tests/index.test.mjs` (pre-existing, unmodified) | Unit | ✅ 1/1 | N/A (pre-existing test, no new behavior) | ✅ Passed (1/1) — confirms `index.mjs` import surface unchanged | ➖ N/A | ➖ None needed |

## Test Summary

- **Total tests written this change (PR1+PR2)**: 38 (32 domain PR1 + 2 application + 2 anthropic-evaluator adapter + 2 pg-evaluation-repository adapter); plus 3 rewritten (not net-new) in `evaluate.test.mjs`
- **Total tests passing**: 88/88 passing + 2 pre-existing skips (full worker suite via `npm test`); `make test-all` fully green across Go + worker + web
- **Layers used**: Unit (all — domain, application, adapters, handler)
- **Approval/characterization tests**: 2 — (1) `evaluation-parser.characterization.test.mjs` (PR1, pins `parseEvaluationResponse` behavior as oracle for `EvaluationParser`), (2) rewritten `evaluate.test.mjs` (PR2, ran green against the OLD handler before reduction, proving the rewrite captures current behavior before refactoring)
- **Pure functions/value objects created**: `Score`, `Evaluation`, `EvaluationParser` (PR1, pure); `EvaluateJob` (PR2, pure orchestration — zero regex/SQL)
- **Mock/assertion ratio**: `EvaluateJob` test uses 2 fakes (evaluator, repository) for 2 tests — well within the ≤3 mock healthy-test threshold; `AnthropicEvaluator`/`PgEvaluationRepository` tests use 1 fake dependency (`tenantQuery` or the 3 lib functions) each

## Full Worker Suite + make test-all (after PR2 work)

`cd worker && npm test` → 18 passed | 1 skipped (19 files), 88 passed | 2 skipped (90 tests). No regressions.
`make test-all` (from repo root) → Go: all packages `ok`. Worker: 88/88 + 2 pre-existing skips. Web: 30/30. RLS (`make test-rls`, Docker-gated) not run — out of scope, no DB schema/migration changes in this slice.

## Files Changed

### PR1 (domain layer — unchanged from prior slice)

| File | Action | What Was Done |
|------|--------|----------------|
| `worker/domain/Score.mjs` | Created | Value object: `Score.of(value)` validates [0,5] or null; throws RangeError (out of range) / TypeError (non-numeric); no threshold logic |
| `worker/domain/Evaluation.mjs` | Created | Domain entity: `fromBlocks`/`parseError` factories; no public constructor; T-58 invariant enforced at construction time |
| `worker/domain/EvaluationParser.mjs` | Created | Pure parser: A-G regex + overall score extraction ported from `parseEvaluationResponse`; never throws; returns `Evaluation` instances |
| `worker/tests/domain/evaluation-parser.characterization.test.mjs` | Created | Inline oracle (verbatim copy of current `parseEvaluationResponse`) + 4 fixtures; proves `EvaluationParser.parse` output is structurally equal to the oracle; also covers never-throw guarantee |
| `worker/tests/domain/score.test.mjs` | Created | 6 zero-mock unit tests for `Score` |
| `worker/tests/domain/evaluation.test.mjs` | Created | 7 zero-mock unit tests for `Evaluation`, including T-58 construction guards |
| `worker/tests/domain/no-mocks.test.mjs` | Created | Structural guard: domain/*.mjs import nothing from lib/db\|anthropic\|prompt; tests/domain/*.test.mjs use no vi.mock |

### PR2 (this slice)

| File | Action | What Was Done |
|------|--------|----------------|
| `worker/adapters/_ports.js` | Created | JSDoc-only `EvaluatorPort` and `EvaluationRepository` contracts, mirroring `providers/_types.js` style |
| `worker/application/EvaluateJob.mjs` | Created | Use case: constructor DI `{evaluator, repository}`, `.run({userId, jobId})` — `evaluator.evaluate → EvaluationParser.parse → repository.save`. Zero regex, zero SQL. |
| `worker/adapters/AnthropicEvaluator.mjs` | Created | Implements `EvaluatorPort` by wrapping `buildEvaluationPrompt` + `evaluate` (lib/prompt.mjs, lib/anthropic.mjs) |
| `worker/adapters/PgEvaluationRepository.mjs` | Created | Implements `EvaluationRepository`; reproduces the 4 SQL writes (applications, reports, usage, jobs) byte-for-byte from the original handler, same order, same values |
| `worker/jobs/evaluate.mjs` | Modified | Reduced from ~150 lines (inline regex parser + 4 inline SQL writes) to a ~25-line thin wiring shim constructing real adapters + the use case |
| `worker/tests/jobs/evaluate.test.mjs` | Modified (rewritten) | Same 3 scenarios (happy path, parse-error guard, empty-response shape), assertions now target specific SQL/param shapes per write instead of a blanket call-count check; used as an approval test against the OLD handler first |
| `worker/tests/application/evaluate-job.test.mjs` | Created | 2 tests: fake evaluator + spy repository, happy path + parse-error path |
| `worker/tests/adapters/anthropic-evaluator.test.mjs` | Created | 2 tests: fakes for `tenantQuery`/`buildEvaluationPrompt`/`evaluate`, happy path + empty-content-blocks edge case |
| `worker/tests/adapters/pg-evaluation-repository.test.mjs` | Created | 2 tests: fake `tenantQuery`, asserts 4 calls/order/SQL fragments/params for both `fromBlocks` and `parseError` evaluations; frozen system time avoids month-boundary flake |
| `openspec/changes/worker-ddd-evaluate/tasks.md` | Modified | Marked T-170..T-187 as `[x]` (merged with PR1's T-157..T-169 already `[x]`) |

**Untouched (by design, scope boundary)**: `worker/jobs/scan.mjs`, `worker/jobs/pdf.mjs`, `worker/jobs/ingest-cv.mjs`, `worker/lib/anthropic.mjs`, `worker/lib/prompt.mjs`, `worker/lib/db.mjs`, `worker/index.mjs`, `web/`.

## Deviations from Design

None — implementation matches design exactly for PR2. (PR1's one minor sequencing simplification — collapsing the two-step `EvaluationParser` build into one — was already noted and carried forward; it did not affect PR2.)

## Issues Found

None. PR2 had no surprises: the rewritten `evaluate.test.mjs` mocks the same 3 external module boundaries (`lib/db.mjs`, `lib/prompt.mjs`, `lib/anthropic.mjs`) as before, because the thin shim still constructs `AnthropicEvaluator`/`PgEvaluationRepository` with those real dependencies — the design's "assert via ports, not call-counting" goal was achieved by moving the granular 4-write/order assertions down into `tests/adapters/pg-evaluation-repository.test.mjs` (which injects a fake `tenantQuery` directly, no `vi.mock` of relative paths) while the handler test keeps coarser per-scenario value assertions as an integration-style smoke check of the wiring.

## Workload / PR Boundary

- Mode: chained PR slice (auto-chain delivery strategy, chain strategy: stacked-to-main, per orchestrator/branch naming `feat/worker-ddd-evaluate-wiring` built directly on `main` which already has PR1 merged)
- Current work unit: Unit 2 — "Ports, EvaluateJob use case, adapters, handler reduction, test rewrite, legacy cleanup"
- Boundary: starts from `main` (PR1's domain/ layer already merged) and ends with the full application+adapters+thin-handler wiring green, dead code removed, `make test-all` fully green
- Estimated review budget impact: ~280 new/changed lines (4 new adapter/application/port files + 3 new test files + 1 modified handler -120/+20 + 1 rewritten test file). Combined with PR1's ~330, total ~610 lines across the 2-PR chain — within the originally forecasted ~520-620 range, split across 2 reviewable PRs as planned.

## Status

31/31 tasks (T-157..T-187) complete across both slices. `make test-all` green (Go + worker + web). Ready for `sdd-verify`.
