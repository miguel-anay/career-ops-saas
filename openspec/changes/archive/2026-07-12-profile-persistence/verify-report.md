# Verify Report: profile-persistence

**Change**: profile-persistence (closes #45)
**Mode**: hybrid (openspec file + engram attempted)
**Scope**: full change on `main` — PR-A #56 (DB migration 007 + worker) + PR-B #58 (Go profile package + web /perfil), both merged.

## Verdict: PASS

No CRITICAL issues. No WARNINGs that block archive. One SUGGESTION (non-blocking).

## Test Evidence (all live-executed, this session)

| Suite | Command | Result |
|---|---|---|
| Worker | `cd worker && npm test` | 215 passed, 4 skipped (DB-integration tests correctly gated), 31/33 files — **PASS** |
| RLS (pgTAP) | `make test-rls` | 4 test files, incl. new `profile_edits_rls.test.sql` (6 assertions) — **PASS** |
| Go unit | `cd api && go test ./... -count=1` | all packages incl. `internal/profile` — **PASS** |
| Go DB-gated integration | `TEST_DATABASE_URL=... go test ./... -count=1` (postgres started via `docker compose up -d postgres`) | `internal/profile` ran `TestApplyOverride_Integration` (2 subtests) and `TestUndoEdit_Integration` (3 subtests, incl. the already-undone/409 case) for real against Postgres — **PASS**, not skipped |
| Web | `npm test -- --run` | 55 passed / 12 files, incl. `__tests__/app/perfil.test.tsx` (mount-render, PATCH+refetch, Undo+refetch) — **PASS** |

Postgres container was stopped again after the DB-gated run (`make test-rls` had already stopped it; restarted for the Go integration run, then stopped again) — no persistent side effect.

## Review-fix confirmation (both already-merged fixes verified present on `main`, not re-litigated)

- **PR-A** (`0dd016a`, ancestor of `main` — confirmed via `git merge-base --is-ancestor`): `worker/jobs/ingest-cv.mjs` — `existingCv` is blanked (`hadGoodCv = rawExistingCv.trim().length > 0 && !existingProfile.parse_error`) when the stored profile is itself a parse-error artifact; `parseErrored` now checks `!profileJson || typeof profileJson !== 'object' || profileJson.parse_error === true`; `markFailed()` helper dedupes the fail+notify sequence; the two pre-Claude `tenantQuery` calls run via `Promise.all`. All four confirmed by direct code read. New tests explicitly cover both fixed edge cases (`does not treat a prior parse-error blob as merge-worthy existing CV`, `treats a non-object profileJson ... as a parse error, never crashes`) and pass.
- **PR-B** (`4c477cc`, ancestor of `main`): `api/internal/profile/service.go` has `ProfileEditView`/`toProfileEditView` (plain JSON, no `pqtype.NullRawMessage`/`sql.NullTime` leak); `web/components/perfil/profile-edit-form.tsx` has the `useEffect(() => setDrafts(...), [profile])` resync; `UndoEdit` checks `edit.Status == "undone"` → `ErrAlreadyUndone` (409, confirmed wired in `handler.go`'s switch); `Servicer` interface lives in `handler.go` (confirmed); `currentKey`/`mergeProfile` are the simplified single-key-lookup / no-error-return versions. All confirmed by direct code read; `TestUndoEdit_Integration/undoing_an_already-undone_edit_...` exercises the 409 path live against Postgres.

## Spec Compliance Matrix

| Domain | Requirement | Evidence | Status |
|---|---|---|---|
| candidate-profile | `GET /api/me/profile` returns shallow merge | `service.go: mergeProfile` + `GetProfile`; `TestMergeProfile_*` unit tests | COMPLIANT |
| candidate-profile | `PATCH` writes override + ledger row atomically | `ApplyOverride` inside one `platform.WithTenantTx`; `TestApplyOverride_Integration/forced_failure_mid-transaction_leaves_neither_write_persisted` | COMPLIANT |
| candidate-profile | Overrides survive re-ingestion | `ingest-cv.mjs`'s `UPDATE users` statement never references `profile_overrides` (by construction — confirmed by reading the SQL) | COMPLIANT (architecturally verified; no single test crosses the worker/Go boundary end-to-end — see Suggestion) |
| candidate-profile | `POST .../undo` reverts override, flips ledger, falls back to `profile_json` | `UndoEdit`; `TestUndoEdit_Integration/owner's_own_undo_...` | COMPLIANT |
| candidate-profile | Cross-tenant undo → 404, no mutation | `GetProfileEdit` is RLS-scoped, `sql.ErrNoRows`→`ErrNotFound`; `TestUndoEdit_Integration/cross-tenant_undo_404s_...` | COMPLIANT |
| candidate-profile | `profile_edits` source/status generic (no CHECK too tight) | migration 007: `CHECK (source IN ('manual','ai_suggestion'))`, `CHECK (status IN ('accepted','proposed','undone'))` — both vocabularies present though unexercised this slice | COMPLIANT |
| candidate-profile | `profile_edits` FORCE RLS, tenant-isolated | `db/rls.sql` + `db/migrations/007...sql`; `profile_edits_rls.test.sql` (6 pgTAP assertions incl. write-path UPDATE/INSERT) — live PASS | COMPLIANT |
| ingest-cv (MODIFIED) | Merge-on-ingest, superset, single Claude call, never-throw parse guard, terminal states | `INGEST_MERGE_SYSTEM_PROMPT`, `buildIngestPrompt`, `parseIngestResponse`, `handleIngestCV`; full worker test file `ingest-cv.test.mjs` (17 tests) — live PASS | COMPLIANT |
| ingest-cv (MODIFIED) | Parse-error over good profile → no overwrite, `failed` status, `notify` fires | D2 guard in `ingest-cv.mjs`; test `sanity guard: parse-error over a good existing profile...` | COMPLIANT |
| ingest-cv (MODIFIED) | Anthropic throw → `failed`, never stuck pending/processing | `try/catch` → `markFailed`; test `Anthropic-throws path...` | COMPLIANT |
| ingest-cv (MODIFIED) | Worker write via `tenantQuery`, no raw pool bypass | test `tenant isolation: every DB write goes through tenantQuery...` | COMPLIANT |
| ingest-cv (MODIFIED) | Row transitions to `processing` before Claude call | test `transitions the row to processing before the Claude call` | COMPLIANT |
| worker-evaluate-job R7 | Evaluation prompt uses effective profile, not raw `profile_json` | `prompt.mjs: mergeProfile` + SELECT `profile_overrides`; `prompt.test.mjs` | COMPLIANT |

## Proposal Success Criteria — re-verified line by line

- [x] Re-ingesting a shorter tailored CV preserves all prior roles — enforced by `INGEST_MERGE_SYSTEM_PROMPT`'s explicit "never drop" rule + superset requirement; behavioral guarantee is prompt-level (LLM-dependent), same ceiling the design doc itself calls out (proposal risk #1, resolved as D1). Not independently falsifiable by a deterministic unit test beyond what exists.
- [x] `GET /api/me/profile` returns the merged effective profile (closes #45) — confirmed live via integration test and code read.
- [x] A `PATCH` edit survives a subsequent CV re-ingestion — confirmed architecturally (ingestion SQL never touches `profile_overrides`); no single test spans both services.
- [x] Undo reverts a field to CV-derived value, marks ledger `undone` — confirmed live.
- [x] Evaluation prompt uses the effective profile — confirmed live (`prompt.test.mjs`).
- [x] `profile_edits` has `FORCE ROW LEVEL SECURITY`; RLS tests pass — confirmed live (`make test-rls`).
- [x] Web `/perfil` renders CV + profile + editable form + Undo-able edits list — confirmed live (`perfil.test.tsx`, 3 passing scenarios) + code read.

All 7 criteria hold.

## Tasks.md vs. code state

All T-267..T-295 checkboxes are `[x]`. Cross-checked against real files (not just trusting the checkboxes) — every task's target file exists with the described content: migration 007, schema.sql/rls.sql mirrors, `profile_edits.sql`/`users.sql` queries, sqlc output, `profile/service.go`+`handler.go`, `main.go` wiring, all 3 web components + page + test. `UpdateUserProfileJSON` has zero remaining callers (`rg` across `.go`/`.mjs`/`.ts`/`.tsx` returns nothing). No task is marked done without matching code.

## TDD Compliance

Tests exist and pass for every RED/GREEN task pair listed in tasks.md (T-274/275, T-276/277, T-278/279, T-280/281, T-282/283, T-284/285, T-286/287), and the two post-merge review fixes each shipped with a *new* test exercising exactly the fixed edge case (`ingest-cv.test.mjs`: stale-parse-error-CV guard, non-object-profileJson guard; `service_test.go`/`rls_integration_test.go`: already-undone 409). Coverage matches tasks.md's claims.

Git history is **not** granular RED-then-GREEN per commit — each unit landed as one `feat(...)` commit bundling tests+implementation (e.g. `6b1c300`, `f85a784`, `d5ed47b`, `d92e8c4`), consistent with this project's established pattern for prior verified changes (`evaluation-quality`, `job-content-fetch`). Since the sub-agent-driven apply flow runs RED then GREEN within a session before committing, and the resulting tests demonstrably drove the implementation (parser/allowlist/atomicity/RLS behavior is all test-covered, not just incidentally exercised), this is treated as compliant with the project's actual TDD convention, not a gap.

## Issues

**CRITICAL**: none.

**WARNING**: none.

**SUGGESTION**:
1. No single automated test exercises the full cross-service path "PATCH an override → run ingest-cv → GET profile still shows the override" — the guarantee currently rests on (a) `ingest-cv.mjs`'s `UPDATE users` statement not referencing `profile_overrides` (code-verified) and (b) each side's independent unit/integration coverage. Low risk given the column is genuinely never written by the worker, but a thin end-to-end test would make the "survives re-ingestion" success criterion self-verifying instead of merely implied by code inspection. Non-blocking.

## Artifact Persistence Note

`mem_save`/`mem_search` tools were not present in this session's available toolset (only `Read`/`Bash`) — engram persistence could not be attempted directly. This report was written to `openspec/changes/profile-persistence/verify-report.md` per hybrid mode's file-based half. The orchestrator should persist this report to engram (`topic_key: sdd/profile-persistence/verify-report`) if cross-session recovery is required.
