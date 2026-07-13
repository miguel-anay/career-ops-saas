# Spec: Candidate Profile (Profile Persistence + Read/Edit API)

> Domain: candidate-profile · Status: complete · Artifact store: openspec

This spec documents the candidate profile capability: reading the effective (merged) profile, applying and undoing manual top-level overrides, and ledger tracking.

## Requirement: `GET /api/me/profile` returns the effective (merged) profile

The API MUST return a shallow merge `{ ...profile_json, ...profile_overrides }` per top-level key, computed at read time, never persisted as a third copy.

#### Scenario: Overrides win over CV-derived fields
- GIVEN a user with `profile_json.target_roles = X` and `profile_overrides.target_roles = Y`
- WHEN they `GET /api/me/profile`
- THEN the response's `target_roles` is `Y`, and every other top-level key not present in `profile_overrides` comes from `profile_json` unchanged

#### Scenario: No overrides yet
- GIVEN a user whose `profile_overrides` is `{}`
- WHEN they `GET /api/me/profile`
- THEN the response is identical to `profile_json`

## Requirement: `PATCH /api/me/profile` writes an override and an accepted ledger row atomically

The API MUST write the given top-level key into `profile_overrides` AND insert a `profile_edits` row with `source = 'manual'`, `status = 'accepted'` in ONE transaction — both or neither.

#### Scenario: Successful edit
- GIVEN an authenticated user PATCHes `{"target_roles": {...}}`
- WHEN the request succeeds
- THEN `users.profile_overrides.target_roles` is updated
- AND a `profile_edits` row exists with `field_path = 'target_roles'`, `old_value` = prior effective value, `new_value` = the new value, `source = 'manual'`, `status = 'accepted'`
- AND both writes happened in the same DB transaction (a failure in either rolls back both)

## Requirement: Manual overrides survive CV re-ingestion

A field present in `profile_overrides` MUST remain the effective value after any subsequent `ingest-cv` run, even though `profile_json` for that same key is fully regenerated.

#### Scenario: Override outlives re-ingestion
- GIVEN a user with `profile_overrides.salary_target = {min: 90000}` (previously PATCHed)
- WHEN a new CV is ingested and `ingest-cv` overwrites `profile_json.salary_target` with a freshly-parsed, different value
- THEN `GET /api/me/profile` still returns `salary_target = {min: 90000}` (the override), not the freshly-regenerated `profile_json.salary_target`

## Requirement: `POST /api/me/profile-edits/{id}/undo` reverts an override

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

## Requirement: `profile_edits` ledger is generic across sources and statuses

The table's `source` and `status` columns MUST NOT be constrained (by schema comment, code enum, or requirement wording) to only `source = 'manual'` / `status ∈ {accepted, undone}`. Future rows such as `source = 'ai_suggestion'`, `status = 'proposed'` MUST be representable without a schema or column-type change.

#### Scenario: A hypothetical non-manual row is schema-valid
- GIVEN the `profile_edits` table as migrated by this change
- WHEN a row is inserted with `source = 'ai_suggestion'`, `status = 'proposed'`
- THEN the insert succeeds against the column types (no CHECK constraint or enum rejects it)

## Requirement: `profile_edits` and profile endpoints are tenant-isolated via RLS

`profile_edits` MUST have `FORCE ROW LEVEL SECURITY` with a tenant policy scoped to `user_id = current_setting('app.current_user_id', true)::uuid`, mirroring `db/rls.sql` conventions (ADR-3). No endpoint in `api/internal/profile/` may rely on an app-layer `WHERE user_id = ?` as the sole tenant boundary.

#### Scenario: pgTAP verifies cross-tenant row invisibility
- GIVEN two `profile_edits` rows owned by users `A` and `B`
- WHEN a pgTAP test sets `app.current_user_id` to `A` and selects from `profile_edits`
- THEN only `A`'s row is visible; `B`'s row is absent

#### Scenario: Cross-tenant PATCH cannot touch another user's row
- GIVEN authenticated user `A`
- WHEN `A` PATCHes `/api/me/profile` (scoped to their own JWT-derived `user_id`, no `user_id` accepted from the body)
- THEN only `A`'s `users` row and `profile_edits` rows are ever written; no request parameter can target another tenant's row
