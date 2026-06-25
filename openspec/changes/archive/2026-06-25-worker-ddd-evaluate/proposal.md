# Proposal: DDD-lite Refactor of the Evaluate-Job Path

## Intent

`worker/jobs/evaluate.mjs` is a 145-line transaction script that mixes three concerns: regex domain parsing (`parseEvaluationResponse`), the Anthropic call, and 4 sequential `tenantQuery` SQL writes. The genuinely tricky piece — the T-58 "never lose the row on parse error" invariant — is currently **untestable in isolation** because reaching it requires mocking DB + Anthropic + prompt. The `providers/` package already proves a clean ports-and-adapters model in this codebase; the evaluate path should mirror it so domain logic has a home and T-58 becomes provable by construction.

## Scope

### In Scope
- Extract a PURE `domain/` layer (no DB, no SDK, no SQL): `Score` value object (validates 0..5, holds the float), `Evaluation` entity that makes T-58 unviolatable (`Evaluation.parseError(raw)` + `Evaluation.fromBlocks(...)` factories), `EvaluationParser` (the A-G block + score-extraction regex).
- Add `application/EvaluateJob` use case orchestrating load → evaluate → parse → persist, with NO regex and NO SQL.
- Add `adapters/`: `AnthropicEvaluator` (wraps lib/anthropic + lib/prompt behind an `EvaluatorPort`), `PgEvaluationRepository` (the 4 writes behind a repository port).
- Reduce `worker/jobs/evaluate.mjs` to a THIN entry adapter that builds the use case and calls it.
- **Characterization test** for the parser using TODAY's `parseEvaluationResponse` output as the oracle, written BEFORE the move.
- **Rewrite** `worker/tests/jobs/evaluate.test.mjs` to assert through the new ports (not relative-path mocks of lib/*), preserving the same observable behavior (4 writes, order, T-58).

### Out of Scope
- `worker/jobs/scan.mjs`, `worker/jobs/pdf.mjs` — pure I/O orchestration, no business rule; do not earn DDD.
- `worker/jobs/ingest-cv.mjs` — identical shape; tracked fast-follow AFTER this pattern is proven. Not in this change.
- Any `RECOMMEND_THRESHOLD` / `isRecommended()` rule — explicitly NOT introduced. `Score` has NO threshold logic.
- The web feature-folder refactor — a separate future SDD change.

## Capabilities

### New Capabilities
- None — pure internal refactor, no new requirement-level behavior.

### Modified Capabilities
- None — behavior is 100% preserved (same payload, same 4 SQL writes, same T-58 persistence, same prompt/model/params).

## Approach

DDD-lite inside the existing hexagonal worker, mirroring `worker/providers/*`. New files under `worker/domain/`, `worker/application/`, `worker/adapters/` (ESM, `type:module`). The use case depends on ports (`EvaluatorPort`, `EvaluationRepositoryPort`); concrete adapters wrap the existing `lib/anthropic`, `lib/prompt`, and `lib/db` `tenantQuery`. The handler becomes a wiring shim. Strict TDD: characterization test first, then move under green.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `worker/domain/` | New | `Score`, `Evaluation`, `EvaluationParser` (pure) |
| `worker/application/EvaluateJob.mjs` | New | Use case orchestration |
| `worker/adapters/` | New | `AnthropicEvaluator`, `PgEvaluationRepository` + ports |
| `worker/jobs/evaluate.mjs` | Modified | Reduced to thin entry adapter |
| `worker/tests/jobs/evaluate.test.mjs` | Modified | Rewritten to assert via new seams |
| `worker/tests/domain/*` | New | Parser/Score/Evaluation unit tests (zero mocks) |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Test rewrite is where behavior-preservation is actually verified; a sloppy rewrite hides a regression | High | Write parser characterization test (oracle = current output) FIRST; keep observable 4-write sequence + T-58 identical |
| Internal call count/order coupling in existing test breaks on restructure | High (by design) | Rewrite to assert through ports, preserving same writes |
| New layering adds files team must learn | Low | Mirrors existing `providers/` vocabulary, not a new paradigm |

## Rollback Plan

Single coherent change on a feature branch (`feat/N-worker-ddd-evaluate`). Revert the merge commit — no DB schema, prompt, or model change means rollback is a pure code revert with zero data/migration impact.

## Dependencies

- None external. Builds only on existing `lib/anthropic`, `lib/prompt`, `lib/db`.

## Success Criteria

- [ ] `make test-all` green.
- [ ] `domain/` layer (Score, Evaluation, EvaluationParser) unit-testable with ZERO mocks, ZERO DB, ZERO API key.
- [ ] evaluate-job behaves identically: same payload, same 4 writes in same order, same T-58 parse-error persistence.
- [ ] `worker/jobs/evaluate.mjs` contains no regex and no SQL.
- [ ] No `RECOMMEND_THRESHOLD` rule introduced anywhere.
