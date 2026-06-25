# Tasks: DDD-lite Refactor of the Evaluate-Job Path

> Task ID range: **T-157 .. T-187** (contiguous, fresh range — highest existing ID in repo is `T-156` in `rls-tenancy-wiring`).
> Strict TDD is ACTIVE. Test command: `make test-all`. Each slice is test-first: RED → GREEN → REFACTOR.

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~520-620 (8 new files ~330 lines, 1 modified handler -120/+15, 1 rewritten test file ~-90/+140, 3 new domain/adapter test files ~220 lines) |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1: domain + characterization oracle (T-157..T-169). PR 2: application + adapters + handler + test rewrite + cleanup (T-170..T-187) |
| Delivery strategy | auto-chain |
| Chain strategy | pending — ask user; default to stacked-to-main given linear, non-rollback-sensitive dependency |

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | Characterization oracle + pure domain layer (Score, Evaluation, EvaluationParser) green, zero mocks | PR 1 | Base: main (or tracker branch if feature-branch-chain). No production wiring changed yet — additive only, safe to merge alone. |
| 2 | Ports, EvaluateJob use case, adapters, handler reduction, test rewrite, legacy cleanup | PR 2 | Base: PR 1's branch (feature-branch-chain) or main (stacked-to-main). Depends on PR 1's domain types existing. |

## Phase 1: Characterization Gate (BEFORE any code moves) — PR 1

- [x] T-157 [test] Create `worker/tests/domain/evaluation-parser.characterization.test.mjs`: copy today's `parseEvaluationResponse` verbatim as a local `oracle()` fn (no import from jobs/evaluate.mjs — inline golden), capture its output for 4 fixtures (full A-G+overall, partial blocks no overall, empty string, malformed/no-headers text) as golden snapshots. Verify: test passes against the inlined oracle itself (sanity).
- [x] T-158 [impl] Add `worker/domain/EvaluationParser.mjs` exporting `EvaluationParser.parse(responseText)` — port the exact regex logic from `parseEvaluationResponse` (block pattern, score extraction, overall score), wrapped in try/catch returning the parse-error shape; do NOT modify `worker/jobs/evaluate.mjs` yet. Verify: file has zero imports from lib/db.mjs, lib/anthropic.mjs, lib/prompt.mjs.
- [x] T-159 [test] Extend T-157's test file to also run the 4 fixtures through `EvaluationParser.parse` and assert structurally equal `blocks`/`score`/`contentMd` against the oracle's golden output. Verify: `npx vitest run worker/tests/domain/evaluation-parser.characterization.test.mjs` green — this is the behavior-preservation gate; do not proceed past Phase 2 until green.

## Phase 2: Pure Domain Layer (zero-mock unit tests) — PR 1

- [x] T-160 [test] Create `worker/tests/domain/score.test.mjs`: assert `Score.of(value)` accepts 0..5 inclusive, accepts `null`, rejects out-of-range (>5, <0) and non-numeric input by throwing `RangeError`/`TypeError` (no clamping/coercion), `.value` getter returns the stored number or null. Verify: test fails (Score.mjs doesn't exist).
- [x] T-161 [impl] Create `worker/domain/Score.mjs` implementing `Score.of(value)` per T-160's contract — explicitly NO `isRecommended()`, NO `RECOMMEND_THRESHOLD`. Verify: T-160 green.
- [x] T-162 [test] Create `worker/tests/domain/evaluation.test.mjs`: assert `Evaluation.fromBlocks(blocks, score, contentMd)` exposes `.blocks`, `.score`, `.contentMd`, `.isParseError === false`, `.statusNote === null`; assert `Evaluation.parseError(raw)` exposes `.blocks === {parse_error:true, raw}`, `.score === null`, `.contentMd === raw`, `.isParseError === true`, `.statusNote === 'Evaluation completed (parse error in blocks)'`. Verify: test fails (Evaluation.mjs doesn't exist).
- [x] T-163 [impl] Create `worker/domain/Evaluation.mjs` implementing both factories and getters per T-162 and the design's "Contracts" section — construction must make T-58 unviolatable (no path to an Evaluation with empty/missing persistable shape). Verify: T-162 green.
- [x] T-164 [test] Add a zero-mock-layer guard assertion (can live in T-160/T-162 files or a small `worker/tests/domain/no-mocks.test.mjs`): grep-style or static import check confirming `worker/domain/*.mjs` import none of `lib/db.mjs`, `lib/anthropic.mjs`, `lib/prompt.mjs`, and the test files use no `vi.mock`. Verify: passes by inspection of import statements.
- [x] T-165 [impl] Refactor `worker/domain/EvaluationParser.mjs` (from T-158) to return `Evaluation.fromBlocks(...)` / `Evaluation.parseError(...)` instead of plain objects, using T-161/T-163's new types. Verify: re-run T-159's characterization test — still green (assert via `.blocks`/`.score`/`.contentMd`/`.isParseError` getters now).
- [x] T-166 [test] Add scenario to `worker/tests/domain/evaluation-parser.characterization.test.mjs`: assert `EvaluationParser.parse('')` (empty) and `.parse('garbled text')` (no headers) both return `Evaluation` instances with `.isParseError === true` and never throw, even if internal regex logic raises. Verify: green.
- [x] T-167 [impl] Wrap `EvaluationParser.parse`'s regex body in try/catch that returns `Evaluation.parseError(responseText)` on any internal exception (mirrors current `parseEvaluationResponse` catch block). Verify: T-166 green.
- [x] T-168 [test] Run full `worker/tests/domain/*` suite standalone: `npx vitest run worker/tests/domain` — confirm zero failures, zero skipped.
- [x] T-169 [chore] Confirm PR 1 boundary: `worker/jobs/evaluate.mjs` is UNTOUCHED at this point (still uses the original inline `parseEvaluationResponse`); domain/ layer exists in parallel, unused by production code. Verify: `git diff --stat worker/jobs/evaluate.mjs` shows no changes since branch start.

## Phase 3: Ports + Application Use Case — PR 2

- [ ] T-170 [impl] Create `worker/adapters/_ports.js` — JSDoc-only `EvaluatorPort {evaluate(userId, jobId): Promise<string>}` and `EvaluationRepository {save(userId, jobId, evaluation): Promise<void>}`, mirroring `worker/providers/_types.js` style (no runtime export besides `export {}`).
- [ ] T-171 [test] Create `worker/tests/application/evaluate-job.test.mjs`: build a fake evaluator (`{ evaluate: vi.fn().mockResolvedValue(rawText) }`) and a spy repository (`{ save: vi.fn() }`), construct `new EvaluateJob({evaluator, repository})`, call `.run({userId, jobId})`, assert `repository.save` was called once with `(userId, jobId, evaluationInstance)` where `evaluationInstance.score`/`.blocks`/`.isParseError` match the parsed rawText. Cover happy path AND parse-error path (empty rawText). Verify: fails (EvaluateJob.mjs doesn't exist).
- [ ] T-172 [impl] Create `worker/application/EvaluateJob.mjs` implementing constructor DI `new EvaluateJob({evaluator, repository})` and `.run({userId, jobId})`: `rawText = await evaluator.evaluate(userId, jobId); evaluation = EvaluationParser.parse(rawText); await repository.save(userId, jobId, evaluation)`. No regex, no SQL. Verify: T-171 green.

## Phase 4: Adapters (production wiring) — PR 2

- [ ] T-173 [test] Create `worker/tests/adapters/anthropic-evaluator.test.mjs`: inject fakes for `tenantQuery`, `buildEvaluationPrompt` (resolves `{system, messages:[{content}]}`), `evaluate` (resolves Anthropic response shape); assert `AnthropicEvaluator.evaluate(userId, jobId)` calls `buildEvaluationPrompt(userId, jobId, {tenantQuery})`, then the injected `evaluate(system, messages[0].content || '')`, and returns `res.content?.[0]?.text || ''`. Verify: fails (AnthropicEvaluator.mjs doesn't exist).
- [ ] T-174 [impl] Create `worker/adapters/AnthropicEvaluator.mjs` implementing `EvaluatorPort` per the design's "Contracts" section, constructor `new AnthropicEvaluator({tenantQuery, buildEvaluationPrompt, evaluate})`. Verify: T-173 green.
- [ ] T-175 [test] Create `worker/tests/adapters/pg-evaluation-repository.test.mjs`: inject a fake `tenantQuery` (`vi.fn()` with `mockResolvedValueOnce` chain), call `PgEvaluationRepository.save(userId, jobId, evaluation)` for BOTH a `fromBlocks` evaluation and a `parseError` evaluation; assert EXACTLY 4 `tenantQuery` calls in order — (1) INSERT applications with `score`, `'Evaluated'`, `evaluation.statusNote`, (2) INSERT reports with `contentMd`, `JSON.stringify(evaluation.blocks)`, (3) UPSERT usage with `month = new Date().toISOString().slice(0,7)` and the `evaluations_count+1` ON CONFLICT clause, (4) UPDATE jobs SET status='evaluated' WHERE id AND user_id. This test inherits the T-58 / 4-write/order assertions from the old `evaluate.test.mjs`. Verify: fails (PgEvaluationRepository.mjs doesn't exist).
- [ ] T-176 [discovery] Freeze time for the usage month assertion: use `vi.setSystemTime(new Date('2026-06-25T00:00:00Z'))` (or equivalent) in `beforeEach`/`afterEach` `vi.useRealTimers()` in T-175's test file so the `YYYY-MM` param assertion never flakes across a real month boundary.
- [ ] T-177 [impl] Create `worker/adapters/PgEvaluationRepository.mjs` implementing `EvaluationRepository` per the design's "Persistence — exact reproduction" section, constructor `new PgEvaluationRepository({tenantQuery})`, reproducing the 4 SQL statements byte-for-byte from current `evaluate.mjs`. Verify: T-175 green (including frozen-time month assertion from T-176).

## Phase 5: Handler Reduction + Wiring — PR 2

- [ ] T-178 [test] Rewrite `worker/tests/jobs/evaluate.test.mjs` from scratch: inject a fake `EvaluatorPort` (`{evaluate: vi.fn().mockResolvedValue(rawText)}`) and a spy `EvaluationRepository` (`{save: vi.fn()}`) via the handler's exported factory/constructor seam (see T-179); assert `repository.save` receives correct `userId`/`jobId`/`Evaluation` for: (a) happy path full A-G+overall response, (b) parse-error path (empty/garbled response). Do NOT mock `lib/db.mjs`/`lib/anthropic.mjs`/`lib/prompt.mjs` via relative-path `vi.mock`, do NOT count `tenantQuery` calls. Verify: fails until T-179 lands.
- [ ] T-179 [impl] Reduce `worker/jobs/evaluate.mjs` to a thin shim: `export async function handleEvaluateJob(job) { const {user_id, job_id} = job.data; const evaluator = new AnthropicEvaluator({tenantQuery, buildEvaluationPrompt, evaluate}); const repository = new PgEvaluationRepository({tenantQuery}); return new EvaluateJob({evaluator, repository}).run({userId: user_id, jobId: job_id}); }`. Remove the inline `parseEvaluationResponse` function and the 4 inline `tenantQuery` calls. Verify: T-178 green; file contains no regex (`grep -c 'RegExp\|/\^\|\\\\d' worker/jobs/evaluate.mjs` → 0) and no `tenantQuery(` SQL literal beyond the import.
- [ ] T-180 [test] Add/confirm `worker/tests/index.test.mjs` (or equivalent bootstrap test) still asserts `evaluate-job` is registered via `handleEvaluateJob` import from `worker/jobs/evaluate.mjs` — no change to `worker/index.mjs` needed. Verify: existing test green, confirming `index.mjs` import surface (`handleEvaluateJob`) is unchanged.

## Phase 6: Full-Suite Verification + Cleanup — PR 2

- [ ] T-181 [test] Run `make test-all` (Go, worker, web, RLS) — confirm fully green including new `worker/tests/domain/*`, `worker/tests/application/*`, `worker/tests/adapters/*`, and rewritten `worker/tests/jobs/evaluate.test.mjs`.
- [ ] T-182 [chore] Grep repo for `RECOMMEND_THRESHOLD` and `isRecommended` across `worker/domain/`, `worker/application/`, `worker/adapters/`, `worker/jobs/evaluate.mjs` — confirm zero matches (spec's "No New Behavior Introduced" requirement).
- [ ] T-183 [chore] Delete the now-dead inline characterization oracle duplication if any was left in `worker/jobs/evaluate.mjs` (should already be gone per T-179); confirm `worker/domain/EvaluationParser.mjs` is the single source of parsing logic.
- [ ] T-184 [test] Confirm `worker/tests/domain/evaluation-parser.characterization.test.mjs` still asserts against the ORIGINAL inlined oracle function (kept as local test fixture, not deleted) — the oracle is a permanent regression guard, not a throwaway. Verify: file still present, still green.
- [ ] T-185 [chore] Verify prompt/model/params unchanged: `buildEvaluationPrompt` call signature and `evaluate(system, content)` call in `AnthropicEvaluator.mjs` match the original `evaluate.mjs` call exactly (same args, same `claude-sonnet-4-6`/`max_tokens: 8000`/`temperature: 0.2` untouched in `lib/anthropic.mjs` — no edits to that file).
- [ ] T-186 [chore] Update proposal/design cross-references if any doc comments in `worker/jobs/evaluate.mjs` still describe the old inline flow — replace the file-level JSDoc comment with a short "thin wiring shim" description pointing to `application/EvaluateJob.mjs`.
- [ ] T-187 [chore] Confirm `worker/jobs/scan.mjs`, `worker/jobs/pdf.mjs`, `worker/jobs/ingest-cv.mjs` are untouched (`git diff --stat` shows no changes) — these are explicitly out of scope; `ingest-cv.mjs`'s identical-shape refactor is a tracked fast-follow, not part of this change.

## Out of Scope Reminders

- No `RECOMMEND_THRESHOLD` / `isRecommended()` — `Score` has no threshold logic (enforced by T-182).
- `worker/jobs/ingest-cv.mjs` mirrors this shape but is a separate fast-follow change — not touched here.
- `worker/jobs/scan.mjs`, `worker/jobs/pdf.mjs` are pure I/O orchestration — no DDD extraction warranted, untouched (T-187).
- `worker/index.mjs` registration is unchanged — still imports `handleEvaluateJob` (T-180).
