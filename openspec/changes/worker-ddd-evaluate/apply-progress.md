# Apply Progress: DDD-lite Refactor of the Evaluate-Job Path

**Slice**: PR1 — Domain layer + characterization gate (T-157..T-169)
**Mode**: Strict TDD
**Branch**: feat/worker-ddd-evaluate-domain
**Status**: 13/13 PR1 tasks complete. PR1 boundary respected — `worker/jobs/evaluate.mjs` untouched.

## Completed Tasks (T-157..T-169)

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

## TDD Cycle Evidence

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|------|-----------|-------|------------|-----|-------|-------------|----------|
| T-157/T-159/T-166 | `worker/tests/domain/evaluation-parser.characterization.test.mjs` | Unit | ✅ 3/3 (evaluate.test.mjs baseline before any change) | ✅ Written | ✅ Passed (12/12) | ✅ 4 fixtures × oracle-equality + 2 never-throw scenarios | ➖ None needed |
| T-160/T-161 | `worker/tests/domain/score.test.mjs` | Unit | N/A (new file) | ✅ Written (failed: Score.mjs missing) | ✅ Passed (6/6) | ✅ 6 cases (bounds, null, type, no-threshold) | ➖ None needed |
| T-162/T-163 | `worker/tests/domain/evaluation.test.mjs` | Unit | N/A (new file) | ✅ Written (failed: Evaluation.mjs missing) | ✅ Passed (7/7) | ✅ 7 cases (fromBlocks happy/null-score, parseError, T-58 construction guards) | ➖ None needed |
| T-164 | `worker/tests/domain/no-mocks.test.mjs` | Unit (static guard) | N/A (new file) | ✅ Written | ✅ Passed (7/7, after excluding self from vi.mock string scan) | ➖ Single (structural check, not behavior) | ✅ Excluded self-file from scan after first run caught a false positive |
| T-158/T-165/T-167 | `worker/domain/EvaluationParser.mjs` (impl, covered by characterization test above) | Unit | N/A (new file) | covered above | covered above | covered above | ➖ None needed |

## Test Summary

- **Total tests written**: 32 (domain suite) — 6 Score, 7 Evaluation, 7 no-mocks guard, 12 characterization
- **Total tests passing**: 32/32 (domain suite); 82/82 passing + 2 pre-existing skips (full worker suite via `npm test`)
- **Layers used**: Unit (32)
- **Approval/characterization tests**: 1 file, 12 tests — pins current `parseEvaluationResponse` behavior as the oracle for `EvaluationParser`
- **Pure functions/value objects created**: `Score` (value object), `Evaluation` (domain entity with 2 factories), `EvaluationParser` (pure parser, no I/O)

## Safety Net Baseline (before any change)

`npx vitest run tests/jobs/evaluate.test.mjs` → 3/3 passing (captured before touching anything). Re-ran after all domain work: still 3/3 passing, file untouched (confirmed via `git diff --stat`).

## Full Worker Suite (after PR1 work)

`cd worker && npm test` → 15 passed | 1 skipped (16 files), 82 passed | 2 skipped (84 tests). No regressions; `tests/jobs/evaluate.test.mjs` (legacy, unmodified) still green.

## Files Changed

| File | Action | What Was Done |
|------|--------|----------------|
| `worker/domain/Score.mjs` | Created | Value object: `Score.of(value)` validates [0,5] or null; throws RangeError (out of range) / TypeError (non-numeric); no threshold logic |
| `worker/domain/Evaluation.mjs` | Created | Domain entity: `fromBlocks`/`parseError` factories; no public constructor; T-58 invariant enforced at construction time |
| `worker/domain/EvaluationParser.mjs` | Created | Pure parser: A-G regex + overall score extraction ported from `parseEvaluationResponse`; never throws; returns `Evaluation` instances |
| `worker/tests/domain/evaluation-parser.characterization.test.mjs` | Created | Inline oracle (verbatim copy of current `parseEvaluationResponse`) + 4 fixtures; proves `EvaluationParser.parse` output is structurally equal to the oracle; also covers never-throw guarantee |
| `worker/tests/domain/score.test.mjs` | Created | 6 zero-mock unit tests for `Score` |
| `worker/tests/domain/evaluation.test.mjs` | Created | 7 zero-mock unit tests for `Evaluation`, including T-58 construction guards |
| `worker/tests/domain/no-mocks.test.mjs` | Created | Structural guard: domain/*.mjs import nothing from lib/db\|anthropic\|prompt; tests/domain/*.test.mjs use no vi.mock |
| `openspec/changes/worker-ddd-evaluate/tasks.md` | Modified | Marked T-157..T-169 as `[x]` |

**Untouched (by design, PR1 boundary)**: `worker/jobs/evaluate.mjs`, `worker/tests/jobs/evaluate.test.mjs`, `worker/lib/anthropic.mjs`, `worker/index.mjs`.

## Deviations from Design

None — implementation matches design. One small sequencing simplification: instead of writing `EvaluationParser.mjs` twice (T-158 returning plain objects, then T-165 refactoring to return `Evaluation` instances), it was implemented once directly returning `Evaluation` instances, since `Score`/`Evaluation` were already built by the time `EvaluationParser` was written and the characterization test (T-159) asserts against `.blocks`/`.score`/`.contentMd` getters either way. Net behavior and test coverage for T-158/T-159/T-165/T-166/T-167 is unchanged; this only collapses two commits worth of intermediate state into one file write.

## Issues Found

None. One self-correcting note: the first version of `no-mocks.test.mjs` flagged itself as containing `vi.mock` (because the guard's own assertion string contains that literal) — fixed by excluding the guard file itself from the scan.

## Remaining Tasks (PR2 — NOT in this slice)

- [ ] T-170 `worker/adapters/_ports.js`
- [ ] T-171 `worker/tests/application/evaluate-job.test.mjs`
- [ ] T-172 `worker/application/EvaluateJob.mjs`
- [ ] T-173 `worker/tests/adapters/anthropic-evaluator.test.mjs`
- [ ] T-174 `worker/adapters/AnthropicEvaluator.mjs`
- [ ] T-175 `worker/tests/adapters/pg-evaluation-repository.test.mjs`
- [ ] T-176 freeze time for month assertion
- [ ] T-177 `worker/adapters/PgEvaluationRepository.mjs`
- [ ] T-178 rewrite `worker/tests/jobs/evaluate.test.mjs`
- [ ] T-179 reduce `worker/jobs/evaluate.mjs` to thin shim
- [ ] T-180 confirm `worker/index.mjs` registration unchanged
- [ ] T-181 full `make test-all` green
- [ ] T-182 grep zero RECOMMEND_THRESHOLD/isRecommended
- [ ] T-183 confirm no dead duplicate parsing logic
- [ ] T-184 confirm oracle test kept as permanent regression guard
- [ ] T-185 confirm prompt/model/params unchanged
- [ ] T-186 update evaluate.mjs file-level doc comment
- [ ] T-187 confirm scan.mjs/pdf.mjs/ingest-cv.mjs untouched

## Workload / PR Boundary

- Mode: chained PR slice (auto-chain delivery strategy, chain strategy pending — default lean stacked-to-main per tasks artifact)
- Current work unit: Unit 1 — "Characterization oracle + pure domain layer (Score, Evaluation, EvaluationParser) green, zero mocks"
- Boundary: starts from branch tip (no domain/ existed) and ends with the full domain/ layer + characterization gate green; `worker/jobs/evaluate.mjs` untouched — safe to merge alone, no production wiring changed
- Estimated review budget impact: ~330 new lines (3 domain files + 4 test files + tasks.md edit), well within the 400-line budget for this slice alone

## Status

13/13 PR1 tasks (T-157..T-169) complete. Ready for orchestrator review before PR2 (T-170..T-187).
