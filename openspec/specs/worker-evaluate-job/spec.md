# Spec: Evaluate-Job Behavior Preservation (DDD-lite Refactor)

## Purpose

This capability spec documents the behavior of `worker/jobs/evaluate.mjs`, the `evaluate-job` pg-boss queue handler for all job-evaluation runs. The implementation is a DDD-lite refactored form (domains: Score, Evaluation, EvaluationParser; application: EvaluateJob; adapters: AnthropicEvaluator, PgEvaluationRepository) that preserves 100% of the original observable behavior while extracting pure domain and application logic testable without test doubles.

## Context

The evaluate-job handler:
1. Receives an `evaluate-job` pg-boss message with `{ user_id, job_id }`
2. Calls Anthropic with a structured prompt to evaluate a job posting against the user's CV
3. Parses the response for structured A-G blocks and an overall score
4. Persists the evaluation (4 SQL writes: applications, reports, usage, jobs) with a critical parse-error invariant: rows are persisted even if parsing fails

## Requirements

### Requirement R1: Identical Side Effects on Successful Evaluation

The system MUST produce the exact same 4 database writes, in the exact same order, with the exact same column values, for any successful (parseable) Anthropic evaluation response.

#### Scenario R1.1: Well-formed A-G response with overall score

- GIVEN an `evaluate-job` payload `{ user_id, job_id }`
- AND Anthropic returns a response text containing all 7 blocks (A-G) and an `**Overall Score: X/5**` line
- WHEN `handleEvaluateJob` (or its equivalent use case) runs
- THEN exactly 4 writes occur in this order:
  1. `INSERT INTO applications` with `score = X`, `status = 'Evaluated'`, `notes = null`
  2. `INSERT INTO reports` with `content_md` = raw response text and `blocks_json` = the parsed A-G object
  3. `UPSERT INTO usage` incrementing `evaluations_count` by 1 for `month = current YYYY-MM`
  4. `UPDATE jobs SET status = 'evaluated'` for the given `job_id`/`user_id`
- AND no other writes occur

#### Scenario R1.2: Partial blocks present but overall score missing

- GIVEN Anthropic returns at least one parseable block but no `**Overall Score: X/5**` line
- WHEN the job runs
- THEN `applications.score` is `null`
- AND `applications.notes` is `null` (no parse-error note, since blocks were extracted)
- AND the 4-write sequence and order are otherwise unchanged

### Requirement R2: T-58 Parse-Error Invariant Preserved By Construction

The system MUST persist the application/report rows even when the Anthropic response is empty or does not contain any recognizable A-G block. This guarantee MUST be enforced by the `Evaluation` domain type itself (it MUST be impossible to construct an `Evaluation` value that has no persistable `blocks`/`score`/`contentMd`).

#### Scenario R2.1: Empty response text

- GIVEN Anthropic returns `content: [{ type: 'text', text: '' }]`
- WHEN the job runs
- THEN `reports.blocks_json` is persisted as `{ parse_error: true, raw: '' }`
- AND `applications.score` is `null`
- AND `applications.notes` equals `'Evaluation completed (parse error in blocks)'`
- AND the job does NOT throw

#### Scenario R2.2: Non-empty but unparseable response text

- GIVEN Anthropic returns response text with no `## Block [A-G]` headers matching the expected pattern (e.g. free-form prose)
- WHEN the job runs
- THEN `reports.blocks_json` is persisted as `{ parse_error: true, raw: <original response text> }`
- AND `applications.score` is `null`
- AND `applications.notes` equals `'Evaluation completed (parse error in blocks)'`
- AND the job does NOT throw

#### Scenario R2.3: Parsing throws an exception

- GIVEN the parsing step raises an exception for any reason while processing the response text
- WHEN the job runs
- THEN the exception MUST be caught internally (by the parser or the `Evaluation` factory) and the same parse-error persistence as above applies — the job does NOT propagate the exception and does NOT skip any of the 4 writes

### Requirement R3: Pure, Mock-Free Domain Layer

The system MUST expose `Score`, `Evaluation`, and `EvaluationParser` as pure domain types/functions importable and unit-testable with zero test doubles, zero database connection, and zero Anthropic API key.

#### Scenario R3.1: Score validates range without threshold logic

- GIVEN a numeric value in `[0, 5]`
- WHEN `Score` is constructed with that value
- THEN construction succeeds and the value is retrievable unchanged
- AND `Score` exposes NO method or property that classifies the value as "recommended" or applies any threshold (e.g. no `isRecommended`, no `RECOMMEND_THRESHOLD`)

#### Scenario R3.2: Score rejects out-of-range or non-numeric values

- GIVEN a value less than 0, greater than 5, `NaN`, or non-numeric
- WHEN `Score` is constructed with that value
- THEN construction MUST fail (throw or return an explicit invalid result, per the implementation's chosen error-handling style) — it MUST NOT silently clamp or coerce to a valid score

#### Scenario R3.3: Evaluation factories cover both outcomes

- GIVEN a parsed result that has at least one block
- WHEN `Evaluation.fromBlocks(blocks, score, contentMd)` is called
- THEN the resulting `Evaluation` exposes `blocks`, `score`, and `contentMd` matching the inputs
- GIVEN raw unparseable text
- WHEN `Evaluation.parseError(raw)` is called
- THEN the resulting `Evaluation` exposes `blocks = { parse_error: true, raw }`, `score = null`, and `contentMd = raw`

#### Scenario R3.4: Domain tests run with no imports of lib/db, lib/anthropic, or lib/prompt

- GIVEN the test files under `worker/tests/domain/*`
- WHEN they are inspected
- THEN none of them import `lib/db.mjs`, `lib/anthropic.mjs`, or `lib/prompt.mjs`, and none of them use `vi.mock`

### Requirement R4: Parser Characterization Against Current Output (Oracle)

The system MUST provide a characterization test that proves the new `EvaluationParser` produces output equal to today's `parseEvaluationResponse` for a representative set of inputs, written before the parser is moved out of `evaluate.mjs`.

#### Scenario R4.1: Representative input set matches oracle output

- GIVEN the following input categories:
  1. A well-formed response with all 7 blocks (A-G) and an overall score
  2. A response with some but not all blocks present
  3. An empty string
  4. A non-empty but malformed response with no recognizable block headers
- WHEN each input is run through both today's `parseEvaluationResponse` (the oracle, captured before refactor) and the new `EvaluationParser`
- THEN for every input the two outputs are structurally equal: same `blocks` keys/values, same `score`, same `contentMd`

### Requirement R5: No New Behavior Introduced

The system MUST NOT introduce any behavior beyond what existed before the refactor.

#### Scenario R5.1: No recommendation threshold exists anywhere

- GIVEN the full diff of this change
- WHEN searched for `RECOMMEND_THRESHOLD` or `isRecommended`
- THEN zero matches are found in `domain/`, `application/`, `adapters/`, or `jobs/evaluate.mjs`

#### Scenario R5.2: Prompt, model, and params unchanged

- GIVEN the Anthropic call made by the new `AnthropicEvaluator` adapter
- WHEN compared to the pre-refactor call in `evaluate.mjs`
- THEN the prompt-building call (`buildEvaluationPrompt`), model (`claude-sonnet-4-6`), `max_tokens`, and `temperature` are unchanged

#### Scenario R5.3: No DB schema change

- GIVEN the SQL statements executed by `PgEvaluationRepository`
- WHEN compared to the pre-refactor 4 `tenantQuery` calls in `evaluate.mjs`
- THEN the same 4 tables (`applications`, `reports`, `usage`, `jobs`), same columns, and same `tenantQuery` tenant-scoping wrapper are used — no new or dropped column, no new table

### Requirement R6: Tests Assert Through Ports, Not Mock Call-Counting

The rewritten `worker/tests/jobs/evaluate.test.mjs` MUST verify behavior by asserting that the use case invokes its `EvaluatorPort` and `EvaluationRepositoryPort` collaborators with the correct domain values, not by mocking `lib/db.mjs` / `lib/anthropic.mjs` / `lib/prompt.mjs` via relative-path `vi.mock` and counting `tenantQuery` calls.

#### Scenario R6.1: Happy path asserted via repository port calls

- GIVEN a fake/mock `EvaluationRepositoryPort` and a fake `EvaluatorPort` injected into the `EvaluateJob` use case
- WHEN the use case runs with a well-formed Anthropic response
- THEN the test asserts the repository port received a `save`/equivalent call with an `Evaluation` (or equivalent value) carrying the correct `score`, `blocks`, and `contentMd` — not raw SQL strings

#### Scenario R6.2: Parse-error path asserted via repository port calls

- GIVEN the fake `EvaluatorPort` returns an empty or unparseable response
- WHEN the use case runs
- THEN the test asserts the repository port received an `Evaluation` with `blocks.parse_error === true` and `score === null`

#### Scenario R6.3: make test-all stays green

- GIVEN the full test suite (`make test-all`: Go, worker, web, RLS)
- WHEN run after the refactor is complete
- THEN all tests pass, including the rewritten `worker/tests/jobs/evaluate.test.mjs` and the new `worker/tests/domain/*` suite

### Requirement R1.2-Extended: `blocks_json` persisted as an array

`PgEvaluationRepository.save` MUST persist `evaluation.blocks` such that `reports.blocks_json` is read back by the API and rendered by the web client as a JSON array of block objects (each with at least a `label` and content), not a plain object. (Previously: `blocks_json` was written as an object keyed by block letter, which the web client's `.map()` silently ignored via the `report.blocks_json.length > 0` guard, so the A-G collapsible UI never rendered.)

#### Scenario: New evaluation persists array-shaped blocks

- GIVEN a completed LLM evaluation with 7 parsed blocks (A-G)
- WHEN `PgEvaluationRepository.save` writes the report
- THEN `reports.blocks_json` is valid JSON array of length 7
- AND each array element carries a block label resolvable by the web client

#### Scenario: Re-evaluation replaces the prior report

- GIVEN an application already has one `reports` row (from a prior evaluation)
- WHEN the job is re-evaluated (e.g., after the user's CV is later ingested)
- THEN the stale report row is deleted and a new one is inserted (per the existing DELETE-then-INSERT flow in `PgEvaluationRepository.save`)
- AND `GetReportByApplicationID` (LIMIT 1, no ORDER BY) returns exactly the new array-shaped report, since only one row exists for the application

#### Scenario: LLM output fails to parse into blocks

- GIVEN the LLM response cannot be parsed into the 7-block structure
- WHEN `PgEvaluationRepository.save` runs
- THEN `blocks_json` is still persisted as a value the web client can safely check with `Array.isArray` / `.length` (e.g., an empty array), never a bare object
- AND the report row is still written (`content_md` retains the raw/fallback text)

### Requirement R1.3-Extended: Prompt includes posting-age signal and STAR/negotiation guidance

`worker/lib/prompt.mjs` MUST include the job's `received_at` age (time since the job was ingested, in human-readable form) as a data point available to Block G, and MUST instruct the model to map CV experience to STAR-format achievements and to include negotiation guidance in its output. This is a prompt-text-only change: the A-G block schema and field names MUST remain unchanged.

#### Scenario: Prompt built for a job with a known `received_at`

- GIVEN a job row with a non-null `received_at` timestamp
- WHEN `buildEvaluationPrompt` constructs the messages array
- THEN the user-content message includes the posting age (e.g., "posted 5 days ago")
- AND the system prompt instructs STAR-mapping and negotiation-guidance generation
- AND the resulting prompt still requests exactly 7 blocks (A-G) with the same field names as before

## Architectural Shape

The evaluate-job handler is implemented as:
- **Domain Layer** (`worker/domain/`): `Score` (value object, [0..5] or null, no threshold logic), `Evaluation` (entity with `fromBlocks`/`parseError` factories, T-58 unviolatable by construction), `EvaluationParser` (pure regex parser, never throws, returns `Evaluation` instances)
- **Application Layer** (`worker/application/`): `EvaluateJob` use case (3-line orchestration: `evaluator.evaluate() → EvaluationParser.parse() → repository.save()`, zero regex/SQL)
- **Adapter Layer** (`worker/adapters/`): `AnthropicEvaluator` (wraps `lib/prompt.mjs` + `lib/anthropic.mjs` behind `EvaluatorPort`), `PgEvaluationRepository` (reproduces 4 SQL writes behind `EvaluationRepositoryPort`), `_ports.js` (JSDoc-only port contracts)
- **Handler** (`worker/jobs/evaluate.mjs`): thin shim (33 lines) wiring adapters and use case

## Files

- **Canonical spec**: `openspec/specs/worker-evaluate-job/spec.md` (this file)
- **Change artifacts**: `openspec/changes/worker-ddd-evaluate/` (proposal, design, tasks, apply-progress, verify-report, archive-report)
- **Implementation**: `worker/domain/`, `worker/application/`, `worker/adapters/`, `worker/jobs/evaluate.mjs` (refactored), `worker/tests/domain/`, `worker/tests/application/`, `worker/tests/adapters/`, `worker/tests/jobs/evaluate.test.mjs` (rewritten)
