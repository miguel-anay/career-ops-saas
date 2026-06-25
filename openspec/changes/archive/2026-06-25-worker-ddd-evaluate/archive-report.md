# Archive Report: Worker DDD-lite Refactor (evaluate-job)

**Change**: worker-ddd-evaluate  
**Status**: CLOSED and MERGED to main  
**Merged PRs**: #23 (domain + characterization), #24 (application + adapters + handler + test rewrite)  
**Spec File**: `openspec/specs/worker-evaluate-job/spec.md` (canonical, now live)  
**Verify Verdict**: PASS (0 CRITICAL, 0 WARNING, 1 SUGGESTION informational)  
**Observation IDs**: #294 (proposal), #295 (spec), #296 (design), #297 (tasks), #299 (verify-report)  

---

## What Was Shipped

A behavior-preserving DDD-lite refactor of `worker/jobs/evaluate.mjs` (145 lines of transaction script) into:

- **Domain Layer** (pure, zero-mock-testable):
  - `Score` (value object: validates [0..5] or null, no threshold logic)
  - `Evaluation` (entity with `fromBlocks`/`parseError` factories, T-58 parse-error invariant unviolatable by construction)
  - `EvaluationParser` (pure regex parser: A-G block extraction + overall score, never throws, returns `Evaluation` instances)

- **Application Layer** (orchestration, zero regex/SQL):
  - `EvaluateJob` use case: `evaluator.evaluate(userId, jobId) → EvaluationParser.parse(rawText) → repository.save(userId, jobId, evaluation)` (3 lines of actual logic)

- **Adapter Layer** (ports + concrete implementations):
  - `EvaluatorPort` / `EvaluationRepositoryPort` (JSDoc-only contracts, mirrors `worker/providers/_types.js`)
  - `AnthropicEvaluator` (wraps `lib/prompt.mjs` + `lib/anthropic.mjs` behind `EvaluatorPort`)
  - `PgEvaluationRepository` (4 SQL writes behind `EvaluationRepositoryPort`, byte-for-byte identical to pre-refactor)

- **Handler** (`worker/jobs/evaluate.mjs` reduced to 33-line thin shim)

**Observable Behavior**: 100% preserved. Identical regexes, identical 4 SQL writes in identical order with identical values, identical T-58 parse-error persistence, identical prompt/model/params, zero new behavior.

---

## Merged PRs

### PR #23 — Domain Layer + Characterization Gate

**Content**: T-157..T-169 (13 tasks)
- `worker/domain/Score.mjs`, `worker/domain/Evaluation.mjs`, `worker/domain/EvaluationParser.mjs` (pure)
- `worker/tests/domain/score.test.mjs`, `worker/tests/domain/evaluation.test.mjs`, `worker/tests/domain/evaluation-parser.characterization.test.mjs`, `worker/tests/domain/no-mocks.test.mjs` (32 tests, all [x])
- Characterization test: inline oracle (verbatim copy of pre-refactor `parseEvaluationResponse`) with 4 fixtures (full A-G+score, partial blocks, empty, malformed) — all assert structural equality against oracle and verify never-throw guarantee

**Test Result**: 32/32 tests green

### PR #24 — Application + Adapters + Handler Reduction + Test Rewrite

**Content**: T-170..T-187 (18 tasks)
- `worker/adapters/_ports.js` (JSDoc contracts)
- `worker/application/EvaluateJob.mjs` (3-line use case)
- `worker/adapters/AnthropicEvaluator.mjs`, `worker/adapters/PgEvaluationRepository.mjs` (adapters)
- `worker/tests/application/evaluate-job.test.mjs`, `worker/tests/adapters/anthropic-evaluator.test.mjs`, `worker/tests/adapters/pg-evaluation-repository.test.mjs` (6 new tests)
- `worker/jobs/evaluate.mjs` reduced to thin shim; `worker/tests/jobs/evaluate.test.mjs` rewritten with port-based assertions (3 tests)
- Frozen system time in adapter test to avoid month-boundary flake

**Test Result**: 88 passed / 2 pre-existing skips (full worker suite green)

**Delivery Mode**: auto-chain, stacked-to-main (PR #24 built on main after PR #23 merged)

---

## Test State (Final)

### Full `make test-all` — All Green

```
Go:      all packages ok
Worker:  88 passed | 2 pre-existing skips (pgboss-real-schema integration tests, unrelated)
         Test Files: 18 passed | 1 skipped (19)
         Tests: 88 passed | 2 skipped (90)
Web:     30 passed
RLS:     35 pgTAP assertions passed (3 files: rls_test.sql 25 ok, cv_ingestions_rls.test.sql 6 ok, nullif_guc.test.sql 4 ok)
```

### Worker Test Breakdown (88 tests)

- `worker/tests/domain/evaluation-parser.characterization.test.mjs`: 12 tests (oracle-equality + never-throw scenarios)
- `worker/tests/domain/score.test.mjs`: 6 tests (range validation, null, no-threshold assertions)
- `worker/tests/domain/evaluation.test.mjs`: 7 tests (factories, T-58 construction guards)
- `worker/tests/domain/no-mocks.test.mjs`: 7 tests (structural guard: zero forbidden imports, zero vi.mock)
- `worker/tests/application/evaluate-job.test.mjs`: 2 tests (happy + parse-error with fakes)
- `worker/tests/adapters/anthropic-evaluator.test.mjs`: 2 tests (fakes for prompt/anthropic)
- `worker/tests/adapters/pg-evaluation-repository.test.mjs`: 2 tests (fake tenantQuery, 4-write/order/values for both outcomes)
- `worker/tests/jobs/evaluate.test.mjs`: 3 tests (rewritten, port-based assertions)
- Pre-existing tests: ~38 tests (unchanged, all green)

---

## Behavior Preservation — Verification Summary

**Verify Phase Verdict**: PASS (0 CRITICAL, 0 WARNING)

| Aspect | Pre-Refactor | Post-Refactor | Status |
|--------|-------------|----------------|--------|
| Block regex | `/##\s+Block\s+([A-G])\s*[—–-]\s*([^\n]+)([\s\S]*?)(?=##\s+Block\s+[A-G]\|##\s+Overall\|\*\*Overall Score\|$)/gi` | Identical in `EvaluationParser.mjs:14` | ✅ |
| Block score regex | `/Score:\s*(\d+(?:\.\d+)?)\s*\/\s*5/i` | Identical `BLOCK_SCORE_PATTERN` | ✅ |
| Overall score regex | `/\*\*Overall Score:\s*(\d+(?:\.\d+)?)\s*\/\s*5\*\*/i` | Identical `OVERALL_SCORE_PATTERN` | ✅ |
| Parse-error persistence | `{parse_error:true, raw}`, score null, notes ternary | Via `Evaluation.parseError` getters | ✅ |
| Write 1: INSERT applications | score, 'Evaluated', notes (ternary) | `(userId, jobId, evaluation.score, evaluation.statusNote)` | ✅ |
| Write 2: INSERT reports | content_md, blocks_json | Identical | ✅ |
| Write 3: UPSERT usage | month `new Date().toISOString().slice(0,7)`, evaluations_count+1 | Byte-for-byte identical | ✅ |
| Write 4: UPDATE jobs | status='evaluated' | Identical | ✅ |
| Write order | 1→2→3→4 via sequencing | 1→2→3→4 via `await` | ✅ |
| Prompt/model call | `buildEvaluationPrompt(user_id, job_id, {tenantQuery})` | Same args, same order | ✅ |
| Model params | claude-sonnet-4-6, max_tokens:8000, temperature:0.2 | `lib/anthropic.mjs` zero diff | ✅ |
| index.mjs registration | Unchanged surface | Zero diff | ✅ |

**Compliance**: 12/12 spec requirements + all 6 design decisions + all 31 tasks (T-157..T-187) verified complete in code.

---

## Design Decisions Carried Forward

| Decision | Rationale | Followed? |
|----------|-----------|-----------|
| Flat siblings `worker/{domain,application,adapters}/` (not `src/`) | Mirrors `worker/jobs/` layout, no build step | ✅ |
| JSDoc-only ports in `adapters/_ports.js` | Matches `worker/providers/_types.js` convention | ✅ |
| Constructor DI, no `vi.mock` of lib/* in new tests | Tests own the mock points, not relative-path imports | ✅ |
| Score: no threshold logic, no `isRecommended`, no `RECOMMEND_THRESHOLD` | Explicitly deferred future behavior | ✅ (grep confirmed 0 matches except 1 negative test + 1 doc comment) |
| T-58 unviolatable by construction | `Evaluation.parseError` factory makes invariant domain-level | ✅ |
| Parser never throws | Internal try/catch → `Evaluation.parseError` | ✅ |
| Characterization oracle test | Kept as permanent regression guard | ✅ (12/12 green, not deleted) |

---

## Blocked/Unresolved Items

None. All 31 tasks complete. Zero blockers or design deviations. One SUGGESTION (mock-heavy observation in `jobs/evaluate.test.mjs` — intentional per design, since the thin shim constructs real adapters; adapter-level test is source of truth).

---

## Follow-Up Work

### (a) Fast-Follow: ingest-cv.mjs DDD-lite Refactor

`worker/jobs/ingest-cv.mjs` has the identical transaction-script shape as the pre-refactor evaluate.mjs:
- Inline regex parsing (CV text block extraction)
- Inline SQL writes (4+ queries)
- No separated domain/application/adapter layers

**Status**: Explicitly OUT OF SCOPE for this change (noted in proposal). Candidate for next SDD change using the proven pattern (domain entity, pure parser, repository port, handler shim). See `openspec/changes/ingest-cv/` for explore + proposal already drafted.

### (b) Web Feature-Folder / Screaming Architecture Refactor

The `web/` layer currently uses a flat route structure under `app/`. A separate SDD change proposes extracting feature-folder ("screaming") architecture to co-locate route + layout + actions.

**Status**: Separate SDD change in progress. See `openspec/changes/worker-ddd-web-screaming/explore.md` for prior exploration.

### (c) Score Recommendation Threshold & isRecommended Rule

The `Score` value object deliberately omits threshold logic and `isRecommended()` method. Future enhancement would:
- Decide threshold value (e.g. 3.5/5)
- Implement `Score.isRecommended()` method
- Add routes/webhook to expose recommendation
- Track in analytics

**Status**: Explicitly deferred. Current `Score` validates [0..5] but makes no recommendations.

---

## Commits & Artifacts

**Engram Observations** (tagged `sdd/worker-ddd-evaluate/` in career-ops-saas project):
- #294: Proposal (sdd/worker-ddd-evaluate/proposal)
- #295: Spec (sdd/worker-ddd-evaluate/spec)
- #296: Design (sdd/worker-ddd-evaluate/design)
- #297: Tasks (sdd/worker-ddd-evaluate/tasks)
- #299: Verify Report (sdd/worker-ddd-evaluate/verify-report)
- *#300*: Archive Report (sdd/worker-ddd-evaluate/archive-report) — THIS FILE

**OpenSpec Files** (openspec/changes/worker-ddd-evaluate/):
- proposal.md, design.md, tasks.md (all complete, marked [x])
- specs/worker-evaluate-job/spec.md (delta spec, now merged into canonical)
- apply-progress.md, verify-report.md (final state documented)
- archive-report.md (this file, now in openspec/ for team reference)

**Canonical Spec** (openspec/specs/worker-evaluate-job/spec.md):
- Requirements R1..R6 (all 12 scenarios verified COMPLIANT by verify phase)
- Architectural shape
- File inventory

**Implementation** (worker/ directory, merged to main):
- `worker/domain/` (3 files, pure)
- `worker/application/` (1 file, orchestration)
- `worker/adapters/` (3 files + ports)
- `worker/jobs/evaluate.mjs` (thin shim, 33 lines)
- `worker/tests/domain/`, `worker/tests/application/`, `worker/tests/adapters/` (9 test files)
- `worker/tests/jobs/evaluate.test.mjs` (rewritten)

---

## Closure Checklist

- [x] All 31 tasks (T-157..T-187) complete and verified in code
- [x] Full `make test-all` green (Go + worker + web + RLS)
- [x] Spec compliance: 12/12 scenarios COMPLIANT
- [x] Behavior preservation: 100% (verified against `git show main:worker/jobs/evaluate.mjs`)
- [x] No new behavior: zero `RECOMMEND_THRESHOLD`/`isRecommended` (grep + negative assertions confirm)
- [x] No changes outside evaluate-job path: `worker/jobs/scan.mjs`, `pdf.mjs`, `ingest-cv.mjs`, `lib/anthropic.mjs`, `lib/prompt.mjs`, `api/`, `web/`, `db/` all zero-diff vs main
- [x] Design decisions all carried forward (ports, DI, domain-level T-58, no threshold, never-throw parser)
- [x] Characterization oracle test kept as regression guard
- [x] Both PRs reviewed, merged to main (#23 → main, #24 → main)
- [x] Canonical spec written to `openspec/specs/worker-evaluate-job/spec.md`
- [x] Archive report persisted to engram (topic_key: `sdd/worker-ddd-evaluate/archive-report`)

**Status**: CLOSED. Change is live on main and archived.

---

## Notes for Future Contributors

1. **Evaluate-Job Handler Entry Point**: Start at `worker/jobs/evaluate.mjs` (thin shim, 33 lines). It constructs `EvaluateJob` use case + `AnthropicEvaluator` + `PgEvaluationRepository`.

2. **Domain Logic**: All business rules live in `worker/domain/` (Score, Evaluation, EvaluationParser). These have zero dependencies on DB/API/external libs and are unit-testable without mocks.

3. **T-58 Invariant**: `Evaluation.parseError(raw)` is the sentinel factory; no Evaluation can exist without persistable blocks/score/contentMd. If you need to refactor persistence, check `PgEvaluationRepository.save()` — that's where the 4 SQL writes live.

4. **Parser as Oracle**: `worker/tests/domain/evaluation-parser.characterization.test.mjs` is the regression guard. If you change parsing logic, this test will catch behavior drift against the oracle.

5. **Fast-Follow Candidate**: `ingest-cv.mjs` has the identical shape. If you refactor it, copy the pattern: domain entity + pure parser + repository port + handler shim.

6. **Threshold Logic**: `Score` intentionally has no `isRecommended()` method. If that becomes a feature, implement it as a new layer (e.g. `Score.toRecommendation(threshold)` or a separate `RecommendationEngine` service).
