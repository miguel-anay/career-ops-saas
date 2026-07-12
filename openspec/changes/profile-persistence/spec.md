# Spec Delta: profile-persistence

## Domain: candidate-profile (NEW)

### Requirement: `GET /api/me/profile` returns the effective (merged) profile

The API MUST return a shallow merge `{ ...profile_json, ...profile_overrides }` per top-level key, computed at read time, never persisted as a third copy.

#### Scenario: Overrides win over CV-derived fields
- GIVEN a user with `profile_json.target_roles = X` and `profile_overrides.target_roles = Y`
- WHEN they `GET /api/me/profile`
- THEN the response's `target_roles` is `Y`, and every other top-level key not present in `profile_overrides` comes from `profile_json` unchanged

#### Scenario: No overrides yet
- GIVEN a user whose `profile_overrides` is `{}`
- WHEN they `GET /api/me/profile`
- THEN the response is identical to `profile_json`

### Requirement: `PATCH /api/me/profile` writes an override and an accepted ledger row atomically

The API MUST write the given top-level key into `profile_overrides` AND insert a `profile_edits` row with `source = 'manual'`, `status = 'accepted'` in ONE transaction — both or neither.

#### Scenario: Successful edit
- GIVEN an authenticated user PATCHes `{"target_roles": {...}}`
- WHEN the request succeeds
- THEN `users.profile_overrides.target_roles` is updated
- AND a `profile_edits` row exists with `field_path = 'target_roles'`, `old_value` = prior effective value, `new_value` = the new value, `source = 'manual'`, `status = 'accepted'`
- AND both writes happened in the same DB transaction (a failure in either rolls back both)

### Requirement: Manual overrides survive CV re-ingestion

A field present in `profile_overrides` MUST remain the effective value after any subsequent `ingest-cv` run, even though `profile_json` for that same key is fully regenerated.

#### Scenario: Override outlives re-ingestion
- GIVEN a user with `profile_overrides.salary_target = {min: 90000}` (previously PATCHed)
- WHEN a new CV is ingested and `ingest-cv` overwrites `profile_json.salary_target` with a freshly-parsed, different value
- THEN `GET /api/me/profile` still returns `salary_target = {min: 90000}` (the override), not the freshly-regenerated `profile_json.salary_target`

### Requirement: `POST /api/me/profile-edits/{id}/undo` reverts an override

Undo MUST remove the corresponding key from `profile_overrides`, flip the ledger row's `status` to `undone` with `resolved_at` set, and cause the effective value to fall back to whatever `profile_json` currently holds for that key.

#### Scenario: Undo restores the CV-derived value
- GIVEN a `profile_edits` row `id = E`, `field_path = 'narrative'`, `status = 'accepted'`, and `users.profile_overrides.narrative` currently set from that edit
- WHEN the owning user `POST`s `/api/me/profile-edits/E/undo`
- THEN `profile_overrides.narrative` key is removed
- AND the `profile_edits` row `E` has `status = 'undone'`, `resolved_at` set
- AND `GET /api/me/profile` now returns `profile_json.narrative` (CV-derived) for that key

#### Scenario: Undo on an edit not owned by the caller
- GIVEN a `profile_edits` row `id = E` owned by user `B`
- WHEN user `A` (different tenant) `POST`s `/api/me/profile-edits/E/undo`
- THEN the API responds `404 Not Found` — RLS makes row `E` invisible to `A`, not an app-layer ownership check
- AND no `profile_overrides` mutation occurs for either user

### Requirement: `profile_edits` ledger is generic across sources and statuses

The table's `source` and `status` columns MUST NOT be constrained (by schema comment, code enum, or requirement wording) to only `source = 'manual'` / `status ∈ {accepted, undone}`. Future rows such as `source = 'ai_suggestion'`, `status = 'proposed'` MUST be representable without a schema or column-type change.

#### Scenario: A hypothetical non-manual row is schema-valid
- GIVEN the `profile_edits` table as migrated by this change
- WHEN a row is inserted with `source = 'ai_suggestion'`, `status = 'proposed'`
- THEN the insert succeeds against the column types (no CHECK constraint or enum rejects it)

### Requirement: `profile_edits` and profile endpoints are tenant-isolated via RLS

`profile_edits` MUST have `FORCE ROW LEVEL SECURITY` with a tenant policy scoped to `user_id = current_setting('app.current_user_id', true)::uuid`, mirroring `db/rls.sql` conventions (ADR-3). No endpoint in `api/internal/profile/` may rely on an app-layer `WHERE user_id = ?` as the sole tenant boundary.

#### Scenario: pgTAP verifies cross-tenant row invisibility
- GIVEN two `profile_edits` rows owned by users `A` and `B`
- WHEN a pgTAP test sets `app.current_user_id` to `A` and selects from `profile_edits`
- THEN only `A`'s row is visible; `B`'s row is absent

#### Scenario: Cross-tenant PATCH cannot touch another user's row
- GIVEN authenticated user `A`
- WHEN `A` PATCHes `/api/me/profile` (scoped to their own JWT-derived `user_id`, no `user_id` accepted from the body)
- THEN only `A`'s `users` row and `profile_edits` rows are ever written; no request parameter can target another tenant's row

## Domain: ingest-cv (MODIFIED)

> Base: `openspec/specs/ingest-cv/spec.md`, Requirement 3.

### Requirement 3 — Worker `ingest-cv` job merges CV content and never overwrites a good profile with a parse failure

The worker MUST call Claude exactly once per job, pass the user's EXISTING `cv_markdown` as context so the model produces a comprehensive superset (never a subset) on write, parse the response for both `cv_markdown` and `profile_json` using a guard that never throws, and always leave the ingestion row in a terminal, inspectable state.
(Previously: the worker always overwrote `cv_markdown`/`profile_json` wholesale from the new response, including writing `{"parse_error": true}` over a previously good `profile_json` on any parse failure.)

#### Scenario: Successful parse — merge-aware happy path
- GIVEN a pg-boss `ingest-cv` job with payload `{user_id, run_id, raw_cv}` and an existing non-null `users.cv_markdown`
- WHEN `handleIngestCV(job)` runs, the ingest prompt is given the prior `cv_markdown`, and Claude returns a merged markdown block plus a valid `profile_json` block
- THEN exactly one Anthropic call is made
- AND the persisted `cv_markdown` is a superset of the prior content (never smaller/less detailed)
- AND `cv_ingestions` transitions to `status = 'completed'`, and `notify(..., 'ingest.completed', ...)` fires

#### Scenario: Shorter tailored CV re-paste preserves older role detail
- GIVEN `users.cv_markdown` already documents roles R1, R2, R3 from a prior full-history paste
- WHEN the user pastes a shorter, job-tailored CV mentioning only an updated R3, and ingestion completes
- THEN the persisted `cv_markdown` still contains R1 and R2 with at least their prior detail, plus R3's update
- AND no previously-recorded role, course, or experience entry is dropped

#### Scenario: Claude response fails to parse — sanity guard preserves prior good values
- GIVEN a pg-boss `ingest-cv` job and a `users` row whose current `cv_markdown`/`profile_json` are non-null and not `{"parse_error": true}`
- WHEN Claude's response cannot be parsed into a valid `cv_markdown` + `profile_json` pair
- THEN the parse guard does NOT throw
- AND `users.cv_markdown` and `users.profile_json` are LEFT UNCHANGED — neither is overwritten with raw/partial text or `{"parse_error": true}`
- AND the `cv_ingestions` row for `run_id` is still updated to `status = 'failed'`, `finished_at` set, and `notify(..., 'ingest.failed', ...)` still fires

#### Scenario: Anthropic call itself throws (network/API error)
- GIVEN a pg-boss `ingest-cv` job
- WHEN the call to `ingestCV(...)` throws (timeout, 5xx, rate limit)
- THEN the `cv_ingestions` row is updated to `status = 'failed'`, `finished_at` set, and `notify(..., 'ingest.failed', ...)` fires
- AND the job handler does not leave the row stuck in `pending`/`processing`

#### Scenario: Worker write respects tenant isolation
- GIVEN a job payload with `user_id = A`
- WHEN the worker performs the `UPDATE users SET cv_markdown=..., profile_json=...` write
- THEN the write goes through `tenantQuery(userId, ...)`, and no raw pool query bypassing RLS exists in the handler

#### Scenario: Job processing transitions status before completion
- GIVEN a `cv_ingestions` row created at enqueue time with `status = 'pending'`
- WHEN `handleIngestCV(job)` begins processing
- THEN the row transitions to `status = 'processing'` before the Claude call completes

## Domain: worker-evaluate-job (MODIFIED)

> Base: `openspec/specs/worker-evaluate-job/spec.md`. This is an ADDED requirement — no existing requirement's behavior changes, so no copy-edit was needed. Archive-time merge: append this requirement block (suggested id `R7`) to the canonical spec above.

### Requirement R7: Evaluation prompt consumes the effective profile, not raw `profile_json`

`worker/lib/prompt.mjs` MUST compute the effective profile (`{ ...profile_json, ...profile_overrides }`, via a small duplicated JS merge fn — no cross-language sharing with the Go merge) before injecting profile data into `buildEvaluationPrompt`. Raw `profile_json` alone MUST NOT be injected when `profile_overrides` is non-empty.

#### Scenario: Manually-overridden target role is reflected in the evaluation
- GIVEN a user with `profile_json.target_roles.primary = ["Backend Engineer"]` and `profile_overrides.target_roles.primary = ["Staff Engineer"]`
- WHEN `evaluate-job` builds the Anthropic prompt for that user
- THEN the prompt's profile data reflects `"Staff Engineer"`, not `"Backend Engineer"`

## Open Questions (not resolved by proposal — flagged, not decided here)

- Proposal's Success Criteria say "PATCH edit survives re-ingestion" but doesn't specify PATCH request/response shape validation (partial vs. full top-level key replacement, unknown-key handling). Left to design phase.
- No explicit requirement in the proposal for what happens if `PATCH` targets a key not in the fixed set (`target_roles`, `salary_target`, `narrative`, `candidate`, `deal_breakers`, `comp_targets`) — not specified here to avoid inventing scope.
