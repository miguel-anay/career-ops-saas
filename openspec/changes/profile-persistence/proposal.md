# Proposal: Profile Persistence + Read/Edit API

## Intent

Today `users.cv_markdown` and `profile_json` are fully overwritten on every CV re-ingestion (`worker/jobs/ingest-cv.mjs`) — re-pasting a job-tailored CV silently drops prior detail, and a malformed Claude response can null a working profile. There is also **no** endpoint or UI to view either field (issue #45). Users need a persistent, editable candidate profile: manual edits must survive re-ingestion and be visible/reversible. This is slice #1 of the `candidate-profile-kb` exploration.

## Scope

### In Scope
- **CV merge-on-ingest** (no schema change): `ingest-cv.mjs` + `ingest-prompt.mjs` merge newly pasted text INTO existing `cv_markdown` (add/update, never drop). Still one `UPDATE users SET cv_markdown = ...` — only the value changes.
- **`profile_overrides jsonb`** column on `users` (migration) — manually-confirmed top-level keys (`target_roles`, `salary_target`, `narrative`, `candidate`, plus new `deal_breakers`, `comp_targets`). Effective profile = shallow merge `{ ...profile_json, ...profile_overrides }` per top-level key.
- **`profile_edits` table** (new, RLS-forced) — ledger: `user_id, field_path, old_value, new_value, source, status, created_at, resolved_at`. Generic enough for future `source: 'ai_suggestion'` / `status: 'proposed'` without a schema change.
- **Go API** (new `api/internal/profile/` package, hexagonal): `GET /api/me/profile` (effective, closes #45), `PATCH /api/me/profile` (writes override + accepted ledger row, one tx), `POST /api/me/profile-edits/{id}/undo` (drops key, flips to `undone`). Retires dead `UpdateUserProfileJSON`.
- **Worker**: `worker/lib/prompt.mjs` consumes effective profile via a small duplicated JS merge fn; output stays a plain injectable JSON blob.
- **Web**: replace `perfil/page.tsx` stub — render `cv_markdown` + effective profile, plain form to edit top-level fields, "Your manual edits" list with per-entry Undo.

### Out of Scope
- Conversational/chat editor + WS wiring → future `conversational-profile-editor` (depends on this).
- Outcome-learning / `matched_archetype` tagging → deferred further.
- `article-digest` → sibling change, no dependency.
- Rich/WYSIWYG editing — plain inputs/textareas only.

## Capabilities

### New Capabilities
- `candidate-profile`: read the effective (merged) profile, apply/undo manual top-level overrides, and the `profile_edits` ledger.

### Modified Capabilities
- `ingest-cv`: `cv_markdown` re-ingestion becomes merge-not-overwrite; ingestion no longer clobbers manual overrides (separate column).
- `worker-evaluate-job`: evaluation prompt consumes the effective profile, not raw `profile_json`.

## Approach

Two independent fixes from exploration Part 2. (A) CV merge is prompt-level only — read existing `cv_markdown`, instruct Claude to produce a comprehensive superset. (B) Manual edits live in a separate `profile_overrides` column, shallow-merged at read time by both the Go read endpoint and the worker (small duplicated merge, no cross-language sharing). `profile_edits` is the single source for "what's overridden and why," powering the visible/undoable list from day one and reused later by the chat editor.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `db/schema.sql`, `db/migrations/` | New | `profile_overrides` column + `profile_edits` table |
| `db/rls.sql` | Modified | `FORCE RLS` + `tenant_profile_edits` policy (mirror `tenant_cvs`) |
| `db/queries/users.sql` | Modified | Merge-aware override query; retire `UpdateUserProfileJSON` |
| `api/internal/profile/` | New | `handler.go`+`service.go`+`repo.go`; 3 endpoints |
| `worker/jobs/ingest-cv.mjs`, `lib/ingest-prompt.mjs` | Modified | Merge-on-ingest |
| `worker/lib/prompt.mjs` | Modified | Consume effective profile |
| `web/app/(app)/perfil/page.tsx` | Modified | Real profile page |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| CV merge hallucinates duplicate/superseded roles (e.g. promotion) | Med | Design-phase prompt with real multi-version CV test cases; not just unit tests on the util |
| Malformed ingest still nulls `profile_json` (pre-existing) | Med | Add sanity guard so a parse-error response does not overwrite a good profile |
| Shallow merge forces whole-key override (can't patch one archetype) | Low | Accepted for first slice; surgical patches deferred to chat editor |
| RLS omission on `profile_edits` = tenant breach | Low | `FORCE ROW LEVEL SECURITY` + policy from day one (ADR-3) |

## Rollback Plan

Migration is additive (new column defaulting `'{}'`, new table) — drop both to revert DB. New `profile/` package and web page are self-contained; delete to remove endpoints/UI. CV merge and prompt changes are prompt/logic reverts with no data migration. No destructive change to existing columns.

## Dependencies

- None external. Requires `cd db && sqlc generate` after query changes.

## Success Criteria

- [ ] Re-ingesting a shorter tailored CV preserves all prior roles in `cv_markdown`.
- [ ] `GET /api/me/profile` returns the merged effective profile (closes #45).
- [ ] A `PATCH` edit survives a subsequent CV re-ingestion.
- [ ] Undo reverts a field to its CV-derived value and marks the ledger row `undone`.
- [ ] Evaluation prompt uses the effective profile.
- [ ] `profile_edits` has `FORCE ROW LEVEL SECURITY`; RLS tests pass.
- [ ] Web `/perfil` renders CV + profile + editable form + Undo-able edits list.
