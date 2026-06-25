# Spec: Evaluate-Job Behavior Preservation (DDD-lite Refactor)

## Purpose

This is an internal refactor with NO new or modified observable capability. The
spec below pins the CURRENT behavior of `worker/jobs/evaluate.mjs` as an
invariant the refactor MUST NOT change, and adds testability requirements for
the new `domain/` layer extracted from it.

## ADDED Requirements

### Requirement: Identical Side Effects on Successful Evaluation

The system MUST produce the exact same 4 database writes, in the exact same
order, with the exact same column values, after the refactor as it did
before — for any successful (parseable) Anthropic evaluation response.

#### Scenario: Well-formed A-G response with overall score

- GIVEN an `evaluate-job` payload `{ user_id, job_id }`
- AND Anthropic returns a response text containing all 7 blocks (A-G) and an
  `**Overall Score: X/5**` line
- WHEN `handleEvaluateJob` (or its equivalent use case) runs
- THEN exactly 4 writes occur in this order: (1) `INSERT INTO applications`
  with `score = X`, `status = 'Evaluated'`, `notes = null`; (2)
  `INSERT INTO reports` with `content_md` = raw response text and
  `blocks_json` = the parsed A-G object; (3) `UPSERT INTO usage` incrementing
  `evaluations_count` by 1 for `month = current YYYY-MM`; (4)
  `UPDATE jobs SET status = 'evaluated'` for the given `job_id`/`user_id`
- AND no other writes occur

#### Scenario: Partial blocks present but overall score missing

- GIVEN Anthropic returns at least one parseable block but no
  `**Overall Score: X/5**` line
- WHEN the job runs
- THEN the `applications.score` value is `null`
- AND `applications.notes` is `null` (no parse-error note, since blocks were
  extracted)
- AND the 4-write sequence and order are otherwise unchanged

### Requirement: T-58 Parse-Error Invariant Preserved By Construction

The system MUST persist the application/report rows even when the Anthropic
response is empty or does not contain any recognizable A-G block, and this
guarantee MUST be enforced by the `Evaluation` domain type itself (it MUST be
impossible to construct an `Evaluation` value that has no persistable
`blocks`/`score`/`contentMd`).

#### Scenario: Empty response text

- GIVEN Anthropic returns `content: [{ type: 'text', text: '' }]`
- WHEN the job runs
- THEN `reports.blocks_json` is persisted as `{ parse_error: true, raw: '' }`
- AND `applications.score` is `null`
- AND `applications.notes` equals `'Evaluation completed (parse error in blocks)'`
- AND the job does NOT throw

#### Scenario: Non-empty but unparseable response text

- GIVEN Anthropic returns response text with no `## Block [A-G]` headers
  matching the expected pattern (e.g. free-form prose)
- WHEN the job runs
- THEN `reports.blocks_json` is persisted as
  `{ parse_error: true, raw: <original response text> }`
- AND `applications.score` is `null`
- AND `applications.notes` equals `'Evaluation completed (parse error in blocks)'`
- AND the job does NOT throw

#### Scenario: Parsing throws an exception

- GIVEN the parsing step raises an exception for any reason while processing
  the response text
- WHEN the job runs
- THEN the exception MUST be caught internally (by the parser or the
  `Evaluation` factory) and the same parse-error persistence as above
  applies — the job does NOT propagate the exception and does NOT skip any
  of the 4 writes

### Requirement: Pure, Mock-Free Domain Layer

The system MUST expose `Score`, `Evaluation`, and `EvaluationParser` as pure
domain types/functions importable and unit-testable with zero test doubles,
zero database connection, and zero Anthropic API key.

#### Scenario: Score validates range without threshold logic

- GIVEN a numeric value in `[0, 5]`
- WHEN `Score` is constructed with that value
- THEN construction succeeds and the value is retrievable unchanged
- AND `Score` exposes NO method or property that classifies the value as
  "recommended" or applies any threshold (e.g. no `isRecommended`, no
  `RECOMMEND_THRESHOLD`)

#### Scenario: Score rejects out-of-range or non-numeric values

- GIVEN a value less than 0, greater than 5, `NaN`, or non-numeric
- WHEN `Score` is constructed with that value
- THEN construction MUST fail (throw or return an explicit invalid result,
  per the implementation's chosen error-handling style) — it MUST NOT
  silently clamp or coerce to a valid score

#### Scenario: Evaluation factories cover both outcomes

- GIVEN a parsed result that has at least one block
- WHEN `Evaluation.fromBlocks(blocks, score, contentMd)` is called
- THEN the resulting `Evaluation` exposes `blocks`, `score`, and `contentMd`
  matching the inputs
- GIVEN raw unparseable text
- WHEN `Evaluation.parseError(raw)` is called
- THEN the resulting `Evaluation` exposes
  `blocks = { parse_error: true, raw }`, `score = null`, and
  `contentMd = raw`

#### Scenario: Domain tests run with no imports of lib/db, lib/anthropic, or lib/prompt

- GIVEN the test files under `worker/tests/domain/*`
- WHEN they are inspected
- THEN none of them import `lib/db.mjs`, `lib/anthropic.mjs`, or
  `lib/prompt.mjs`, and none of them use `vi.mock`

### Requirement: Parser Characterization Against Current Output (Oracle)

The system MUST provide a characterization test that proves the new
`EvaluationParser` produces output equal to today's `parseEvaluationResponse`
for a representative set of inputs, written before the parser is moved out of
`evaluate.mjs`.

#### Scenario: Representative input set matches oracle output

- GIVEN the following input categories: (1) a well-formed response with all
  7 blocks (A-G) and an overall score; (2) a response with some but not all
  blocks present; (3) an empty string; (4) a non-empty but malformed
  response with no recognizable block headers
- WHEN each input is run through both today's `parseEvaluationResponse` (the
  oracle, captured before refactor) and the new `EvaluationParser`
- THEN for every input the two outputs are structurally equal:
  same `blocks` keys/values, same `score`, same `contentMd`

### Requirement: No New Behavior Introduced

The system MUST NOT introduce any behavior beyond what existed before the
refactor.

#### Scenario: No recommendation threshold exists anywhere

- GIVEN the full diff of this change
- WHEN searched for `RECOMMEND_THRESHOLD` or `isRecommended`
- THEN zero matches are found in `domain/`, `application/`, `adapters/`, or
  `jobs/evaluate.mjs`

#### Scenario: Prompt, model, and params unchanged

- GIVEN the Anthropic call made by the new `AnthropicEvaluator` adapter
- WHEN compared to today's call in `evaluate.mjs`
- THEN the prompt-building call (`buildEvaluationPrompt`), model
  (`claude-sonnet-4-6`), `max_tokens`, and `temperature` are unchanged

#### Scenario: No DB schema change

- GIVEN the SQL statements executed by `PgEvaluationRepository`
- WHEN compared to today's 4 `tenantQuery` calls in `evaluate.mjs`
- THEN the same 4 tables (`applications`, `reports`, `usage`, `jobs`), same
  columns, and same `tenantQuery` tenant-scoping wrapper are used — no new
  or dropped column, no new table

### Requirement: Tests Assert Through Ports, Not Mock Call-Counting

The rewritten `worker/tests/jobs/evaluate.test.mjs` MUST verify behavior by
asserting that the use case invokes its `EvaluatorPort` and
`EvaluationRepositoryPort` collaborators with the correct domain values, not
by mocking `lib/db.mjs` / `lib/anthropic.mjs` / `lib/prompt.mjs` via
relative-path `vi.mock` and counting `tenantQuery` calls.

#### Scenario: Happy path asserted via repository port calls

- GIVEN a fake/mock `EvaluationRepositoryPort` and a fake `EvaluatorPort`
  injected into the `EvaluateJob` use case
- WHEN the use case runs with a well-formed Anthropic response
- THEN the test asserts the repository port received a `save`/equivalent
  call with an `Evaluation` (or equivalent value) carrying the correct
  `score`, `blocks`, and `contentMd` — not raw SQL strings

#### Scenario: Parse-error path asserted via repository port calls

- GIVEN the fake `EvaluatorPort` returns an empty or unparseable response
- WHEN the use case runs
- THEN the test asserts the repository port received an `Evaluation` with
  `blocks.parse_error === true` and `score === null`

#### Scenario: make test-all stays green

- GIVEN the full test suite (`make test-all`: Go, worker, web, RLS)
- WHEN run after the refactor is complete
- THEN all tests pass, including the rewritten
  `worker/tests/jobs/evaluate.test.mjs` and the new
  `worker/tests/domain/*` suite
