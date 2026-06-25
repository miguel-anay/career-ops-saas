# Design: DDD-lite Refactor of the Evaluate-Job Path

## Technical Approach

Extract the domain logic trapped inside `worker/jobs/evaluate.mjs` into a pure
domain layer, an application use case, and two adapters behind ports — mirroring
the existing `worker/providers/*` ports-and-adapters model. Behavior is 100%
preserved: same payload, same prompt/model/params, same 4 SQL writes in the same
order, same T-58 parse-error persistence. The handler becomes a wiring shim.
Strict TDD: parser characterization test (oracle = current `parseEvaluationResponse`)
goes green BEFORE the move.

## Architecture Decisions

| Decision | Choice | Rejected | Rationale |
|---|---|---|---|
| Folder layout | Flat siblings under `worker/`: `domain/`, `application/`, `adapters/` | `worker/src/evaluation/...` | Worker has NO build step and uses flat `jobs/ lib/ providers/`. `src/` would orphan existing dirs and break `tests/` mirror convention. |
| Ports | Duck-typed ESM shapes documented in `adapters/_ports.js` JSDoc | TS interfaces / zod | Mirrors `providers/_types.js` (JSDoc-only, `_`-prefixed, runtime-enforced by caller). No new tooling. |
| DI mechanism | Constructor injection: `new EvaluateJob({ evaluator, repository })` | `vi.mock` of relative paths | Tests inject plain fakes — no module mocking. The current test's brittleness IS the `vi.mock` coupling. |
| Score | Value object, validates `0..5`, NO threshold | add `isRecommended()` | Proposal forbids RECOMMEND_THRESHOLD. Score stays a dumb invariant-holder. |
| T-58 | `Evaluation.parseError(raw)` factory makes the sentinel a domain construct | guard in handler | Makes "never lose the row" unviolatable by construction and unit-testable with zero mocks. |
| Parser never throws | `EvaluationParser.parse()` catches internally, returns `Evaluation.parseError` | let handler try/catch | Centralizes T-58 in domain; handler has no regex/try-catch. |

## Data Flow

    pg-boss "evaluate-job"
       │  { user_id, job_id }
       ▼
    jobs/evaluate.mjs (shim) ── constructs ──► EvaluateJob({ evaluator, repository })
       │                                            │
       │                          run({userId,jobId})│
       │                                            ▼
       │         evaluator.evaluate(userId,jobId) ─► rawText   (AnthropicEvaluator → lib/prompt + lib/anthropic)
       │                                            ▼
       │         EvaluationParser.parse(rawText)  ─► Evaluation (pure domain)
       │                                            ▼
       │         repository.save(userId,jobId,evaluation)      (PgEvaluationRepository → 4 tenantQuery writes)

## File Changes

| File | Action | Description |
|---|---|---|
| `worker/domain/Score.mjs` | Create | Value object, validate `0..5` or `null`. |
| `worker/domain/Evaluation.mjs` | Create | Entity + `fromBlocks` / `parseError` factories + getters. |
| `worker/domain/EvaluationParser.mjs` | Create | A–G + overall-score regex; returns `Evaluation`, never throws. |
| `worker/adapters/_ports.js` | Create | JSDoc-only `EvaluatorPort`, `EvaluationRepository` contracts. |
| `worker/adapters/AnthropicEvaluator.mjs` | Create | Wraps `lib/prompt` + `lib/anthropic`; returns raw text. |
| `worker/adapters/PgEvaluationRepository.mjs` | Create | The 4 writes + current-month logic via `tenantQuery`. |
| `worker/application/EvaluateJob.mjs` | Create | Use case; orchestrates, no regex/SQL. |
| `worker/jobs/evaluate.mjs` | Modify | Thin shim: build adapters, `run()`. Drop regex + SQL. |
| `worker/tests/domain/*.test.mjs` | Create | Zero-mock unit tests + parser characterization. |
| `worker/tests/jobs/evaluate.test.mjs` | Modify | Assert via injected fake evaluator + spy repository. |

## Interfaces / Contracts

```js
// domain/Score.mjs
export class Score {
  static of(value) { /* null passes; else Number 0..5 or throws RangeError */ }
  get value() { return this._value }   // number | null
}

// domain/Evaluation.mjs
export class Evaluation {
  static fromBlocks(blocks, score, contentMd) {}   // score: Score
  static parseError(rawText) {}                     // blocks {parse_error:true, raw}, score null
  get blocks() {}        // object persisted as blocks_json
  get score() {}         // number | null  (Score.value)
  get contentMd() {}     // string
  get isParseError() {}  // boolean
  get statusNote() {}    // 'Evaluation completed (parse error in blocks)' | null
}

// domain/EvaluationParser.mjs
export const EvaluationParser = {
  parse(responseText) { /* A–G regex + overall; try/catch → Evaluation.parseError; returns Evaluation */ }
}

// adapters/_ports.js  (JSDoc duck-typed)
/** @typedef {{ evaluate(userId, jobId): Promise<string> }} EvaluatorPort */
/** @typedef {{ save(userId, jobId, evaluation): Promise<void> }} EvaluationRepository */
```

```js
// application/EvaluateJob.mjs
export class EvaluateJob {
  constructor({ evaluator, repository }) { this.evaluator = evaluator; this.repository = repository }
  async run({ userId, jobId }) {
    const rawText    = await this.evaluator.evaluate(userId, jobId)
    const evaluation = EvaluationParser.parse(rawText)
    await this.repository.save(userId, jobId, evaluation)
  }
}
```

```js
// adapters/AnthropicEvaluator.mjs
export class AnthropicEvaluator {
  constructor({ tenantQuery, buildEvaluationPrompt, evaluate }) { /* injected for testability */ }
  async evaluate(userId, jobId) {
    const p = await this.buildEvaluationPrompt(userId, jobId, { tenantQuery: this.tenantQuery })
    const res = await this.runModel(p.system, p.messages?.[0]?.content || '')
    return res.content?.[0]?.text || ''
  }
}

// jobs/evaluate.mjs (shim)
export async function handleEvaluateJob(job) {
  const { user_id, job_id } = job.data
  const evaluator  = new AnthropicEvaluator({ tenantQuery, buildEvaluationPrompt, evaluate })
  const repository = new PgEvaluationRepository({ tenantQuery })
  await new EvaluateJob({ evaluator, repository }).run({ userId: user_id, jobId: job_id })
}
```

`worker/index.mjs` is unchanged — it still imports `handleEvaluateJob` and
registers it on `evaluate-job`.

## Persistence Adapter — Exact Reproduction (confirmed)

`PgEvaluationRepository.save(userId, jobId, evaluation)` runs the SAME 4
`tenantQuery` calls, SAME order, SAME values as today:

1. INSERT `applications` (user_id, job_id, **score=`evaluation.score`**, status `'Evaluated'`, notes=`evaluation.statusNote`) RETURNING id.
2. INSERT `reports` (user_id, application_id, **content_md=`evaluation.contentMd`**, **blocks_json=`JSON.stringify(evaluation.blocks)`**).
3. UPSERT `usage` (month=`new Date().toISOString().slice(0,7)`, evaluations_count=1, ON CONFLICT `evaluations_count = usage.evaluations_count + 1`).
4. UPDATE `jobs SET status='evaluated' WHERE id AND user_id`.

`statusNote` reproduces the existing ternary: parse-error → the note string, else `null`.

## Testing Strategy

| Layer | What | Approach |
|---|---|---|
| Domain | `Score` 0..5 + null | Zero-mock unit. |
| Domain | `Evaluation` factories/getters incl. T-58 | Zero-mock unit. |
| Domain | `EvaluationParser` A–G + overall + empty/garbled | **Characterization**: oracle = current `parseEvaluationResponse`. Capture golden outputs for the 3 existing test fixtures BEFORE the move; assert parser yields identical `blocks/score/contentMd/isParseError`. |
| Application | `EvaluateJob.run` orchestration | Fake `evaluator` returning canned text; **spy `repository.save`** — assert it receives the right `Evaluation` (blocks/score/isParseError). No regex/SQL touched. |
| Adapter | `PgEvaluationRepository.save` | Inject fake `tenantQuery`; assert 4 calls, order, SQL fragments, params (score, statusNote, blocks_json, month) — replaces the old call-count coupling but now scoped to the adapter, not the use case. |

Characterization detail: temporarily export the legacy `parseEvaluationResponse`
in a scratch test fixture (or inline golden snapshots) so the parser diff is
provable; delete the legacy function only once the characterization test is green
against `EvaluationParser`.

## Migration / Rollout

No migration. No DB schema, prompt, model, or payload change. Single coherent
change; revert = code revert, zero data impact.

## Open Questions

- None blocking. `ingest-cv.mjs` mirrors this shape but is explicitly out of
  scope (fast-follow after the pattern is proven).
