# Tasks: Profile Persistence + Read/Edit API

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~650-800 (migration ~60, queries ~40, Go package ~220, worker changes ~180, web page+3 components ~250, RLS/web tests ~150) |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR-A: DB + worker (merge fix + effective profile) — T-267..T-282. PR-B: Go API + web (read/edit/undo) — T-283..T-299 |
| Delivery strategy | ask-on-risk |
| Chain strategy | stacked-to-main |

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High

**Recommendation (acted on directly, per orchestrator instruction not to pause):** ship as 2 chained PRs, `stacked-to-main`, matching the `evaluation-quality` precedent. PR-A (DB migration + queries + worker merge/guard/effective-profile) is independently mergeable and testable via `make test-worker` + `make test-rls` — it changes production behavior (safer ingestion) even with no UI yet. PR-B (Go `profile` package + web `/perfil`) depends on PR-A's schema/queries but is otherwise self-contained. Splitting here keeps each PR's diff near/under ~400 lines and lets PR-A ship (and de-risk the ingest bug fix) before the read/edit surface exists.

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | Migration 007 + queries + sqlc + CV merge fix + sanity guard + worker effective-profile + RLS pgTAP | PR-A (main) | Base: main. No UI dependency. |
| 2 | Go `profile` package + `main.go` wiring + web `/perfil` + web tests | PR-B (main) | Base: main, after PR-A merges. Depends on migration 007 + `GetUserProfile`/`SetProfileOverrideKey`/`DropProfileOverrideKey` from PR-A. |

## Phase 1: Database (Unit 1)

- [x] T-267 `db/migrations/007_profile_persistence.sql` — add `users.profile_overrides jsonb NOT NULL DEFAULT '{}'`, create `profile_edits` table (D4 exact DDL: columns, `CHECK` on `source`/`status`, index), `ENABLE`+`FORCE ROW LEVEL SECURITY`, `tenant_profile_edits` policy (NULLIF-hardened), `GRANT ... TO app_user`.
- [x] T-268 `db/schema.sql` — mirror the migration: add `profile_overrides` column to `users`, add `profile_edits` table definition, canonical and in sync with 007.
- [x] T-269 `db/rls.sql` — add `ALTER TABLE profile_edits ENABLE/FORCE ROW LEVEL SECURITY` + `tenant_profile_edits` policy, matching `tenant_cvs` shape.
- [x] T-270 `db/queries/profile_edits.sql` (new) — `InsertProfileEdit`, `ListProfileEditsByUser`, `GetProfileEdit`, `MarkProfileEditUndone` per design D4.
- [x] T-271 `db/queries/users.sql` — remove `UpdateUserProfileJSON`; add `GetUserProfile`, `SetProfileOverrideKey`, `DropProfileOverrideKey` per design D4.
- [x] T-272 Run `cd db && sqlc generate`; commit regenerated `api/internal/db/*` output (no hand-edits).

**Acceptance (T-267..T-272)**: migration applies cleanly on top of 006; `db/schema.sql`/`db/rls.sql` match the migration; `sqlc generate` produces no diff-drift on re-run; `UpdateUserProfileJSON` has no remaining callers.

## Phase 2: pgTAP RLS Test (Unit 1)

- [x] T-273 `db/tests/profile_edits_rls.test.sql` (new, mirrors `db/tests/cv_ingestions_rls.test.sql`) — assert `profile_edits` `relrowsecurity`/`relforcerowsecurity` true; user B cannot SELECT/UPDATE user A's row; user B cannot INSERT a row claiming user A's `user_id` (`WITH CHECK` denies, `42501`).

**Acceptance (T-273)**: `make test-rls` passes, including the new suite.

## Phase 3: Worker — CV Merge-on-Ingest (Unit 1)

- [ ] T-274 RED — `worker/tests/lib/ingest-prompt.test.mjs`: test `buildIngestPrompt(rawCV, existingCvMarkdown)` — when `existingCvMarkdown` is non-empty, returns the `INGEST_MERGE_SYSTEM_PROMPT` system block and a user message containing both the existing CV and new text labeled distinctly; when empty, behaves exactly as today (`INGEST_SYSTEM_PROMPT`, raw-only user message).
- [ ] T-275 GREEN — `worker/lib/ingest-prompt.mjs`: add `INGEST_MERGE_SYSTEM_PROMPT` (exact text per design D1) and change `buildIngestPrompt` signature to `(rawCV, existingCvMarkdown)`, branching on presence.
- [ ] T-276 RED — `worker/tests/jobs/ingest-cv.test.mjs`: (a) merge case — pre-existing `cv_markdown`/`profile_json` are read and passed into `buildIngestPrompt`; (b) parse-error over a good profile skips the `UPDATE users`, marks `cv_ingestions` `failed`, calls `notify(..., 'ingest.failed', {error: 'parse_error_preserved_existing'})`; (c) parse-error over an empty/absent prior profile still performs the `UPDATE` (today's first-ingest behavior unchanged).
- [ ] T-277 GREEN — `worker/jobs/ingest-cv.mjs`: pre-read `cv_markdown, profile_json` via `tenantQuery` before building the prompt; add the D2 sanity guard immediately before `UPDATE users`.

**Acceptance (T-274..T-277)**: `make test-worker` green; a shorter tailored CV re-paste preserves prior role detail (per spec scenario); a parse error never overwrites a good profile.

## Phase 4: Worker — Effective Profile in Evaluation Prompt (Unit 1)

- [ ] T-278 RED — `worker/tests/lib/prompt.test.mjs`: `mergeProfile(profileJson, profileOverrides)` — override key wins over `profile_json`, non-overridden keys pass through unchanged, handles both string and object inputs.
- [ ] T-279 GREEN — `worker/lib/prompt.mjs`: add `mergeProfile` (D3 exact 4-line JS merge), extend the user SELECT to include `profile_overrides`, feed `JSON.stringify(mergeProfile(...))` into `cvAndProfileBlock` in place of raw `profileJson`.

**Acceptance (T-278..T-279)**: `buildEvaluationPrompt` output reflects an overridden `target_roles` over the raw `profile_json` value (spec scenario, Domain worker-evaluate-job R7).

## Phase 5: Go API — `profile` package (Unit 2)

- [ ] T-280 RED — `api/internal/profile/service_test.go`: `mergeProfile(base, overrides []byte)` — override key replaces the whole top-level key; non-overridden keys pass through; empty/nil inputs don't panic.
- [ ] T-281 GREEN — `api/internal/profile/service.go`: implement `mergeProfile` (D3 exact Go func) + `Servicer` interface (`GetProfile`, `ApplyOverride`, `UndoEdit`) + `NewService(pool)`.
- [ ] T-282 RED — `api/internal/profile/service_test.go`: `ApplyOverride` rejects a `fieldPath` outside the allowlist (`target_roles`, `salary_target`, `narrative`, `candidate`, `deal_breakers`, `comp_targets`) with a 400-mapped error, before touching the DB.
- [ ] T-283 GREEN — `api/internal/profile/service.go`: add the allowlist check at the top of `ApplyOverride`.
- [ ] T-284 RED — `api/internal/profile/rls_integration_test.go` (mirrors `cv/rls_integration_test.go`, DB-gated via `TEST_DATABASE_URL`): `ApplyOverride` writes the override key AND inserts one `profile_edits` row atomically in a single `WithTenantTx` (both committed together); a forced failure mid-transaction leaves neither write persisted.
- [ ] T-285 GREEN — `api/internal/profile/service.go`: implement `ApplyOverride` per design D5 (`GetUserProfile` → compute `oldVal` → `SetProfileOverrideKey` → `InsertProfileEdit`, one tx).
- [ ] T-286 RED — same integration test file: `UndoEdit` on another tenant's `editID` returns not-found (RLS-scoped `GetProfileEdit` returns `sql.ErrNoRows`), with no `profile_overrides` mutation for either user; `UndoEdit` on the caller's own edit drops the override key and flips the ledger row to `undone`.
- [ ] T-287 GREEN — `api/internal/profile/service.go`: implement `UndoEdit` per design D5.
- [ ] T-288 `api/internal/profile/handler.go` (new) — `Handler` struct wrapping `Servicer`; `RegisterRoutes` for `GET /api/me/profile`, `PATCH /api/me/profile`, `POST /api/me/profile-edits/{id}/undo`; request/response shapes per design D5 (`EffectiveProfile`, PATCH body `{field_path, value}`, undo `204`).
- [ ] T-289 `api/cmd/api/main.go` — wire `profileHandler := profile.NewHandler(profile.NewService(pool)); profileHandler.RegisterRoutes(r)` alongside the other handlers.

**Acceptance (T-280..T-289)**: `make test-go` green; `GET /api/me/profile` returns the merged effective profile; a cross-tenant undo 404s; PATCH+ledger insert is all-or-nothing.

## Phase 6: Web — `/perfil` page (Unit 2)

- [ ] T-290 `web/components/perfil/cv-markdown-view.tsx` (new) — `CvMarkdownView`: read-only render of `cv_markdown` (reuse the report view's markdown renderer if one exists; plain `<pre>` fallback).
- [ ] T-291 `web/components/perfil/profile-edit-form.tsx` (new) — `ProfileEditForm`: plain inputs/textareas for the 6 allowlisted top-level keys; on save calls `apiPatch('/api/me/profile', {field_path, value})`, then triggers parent refetch.
- [ ] T-292 `web/components/perfil/manual-edits-list.tsx` (new) — `ManualEditsList`: renders `edits` (status `accepted`), each row with an Undo button calling `apiPost('/api/me/profile-edits/{id}/undo')`, then triggers parent refetch.
- [ ] T-293 `web/app/(app)/perfil/page.tsx` (modify) — replace the `ComingSoon` stub with a client component: `apiGet('/api/me/profile')` on mount, local `useState` for profile/edits/loading, compose the three components above, refetch-on-mutation.
- [ ] T-294 `web/__tests__/app/perfil.test.tsx` (new, structural pattern per `companies.test.tsx`) — mocks `apiGet`/`apiPatch`/`apiPost`: (a) renders CV markdown + profile fields on mount; (b) submitting the edit form calls `apiPatch` and the edits list updates; (c) clicking Undo calls `apiPost(.../undo)` and the list reflects the revert.

**Acceptance (T-290..T-294)**: `make test-web` green; page renders CV + effective profile + editable form + undoable edits list per spec's web requirement.

## Phase 7: Cross-cutting Verification

- [ ] T-295 Run `make test-all` (Go + worker + web + RLS) and confirm all suites are green before opening either PR.

## Dependencies Between Units

- Unit 1 (T-267..T-279) has no dependency on Unit 2 — ships and merges to `main` first.
- Unit 2 (T-280..T-294) depends on Unit 1's migration 007 (`profile_overrides` column, `profile_edits` table) and the 3 new/changed sqlc queries (`GetUserProfile`, `SetProfileOverrideKey`, `DropProfileOverrideKey`, `InsertProfileEdit`, `GetProfileEdit`, `MarkProfileEditUndone`) — cannot start meaningfully until Unit 1 is on `main`.
- T-295 runs after both units are merged (or, if run per-PR, scoped to that PR's touched suites).
