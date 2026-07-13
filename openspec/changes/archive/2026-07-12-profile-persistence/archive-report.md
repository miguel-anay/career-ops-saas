# Archive Report: profile-persistence

**Change**: profile-persistence  
**Closed Issue**: #45 (Profile Persistence + Read/Edit API)  
**Archived**: 2026-07-12  
**Status**: COMPLETE  
**Verdict**: PASS (0 CRITICAL, 0 WARNING, 1 non-blocking SUGGESTION)

## Change Summary

The profile-persistence change implements persistent, editable candidate profiles with three independent fixes:

1. **CV merge-on-ingest** (no schema change): `ingest-cv.mjs` reads existing `cv_markdown` and instructs Claude to produce a comprehensive superset, never dropping prior detail.
2. **Manual edits column** (`profile_overrides` jsonb): new column on `users` for user-confirmed profile overrides, shallow-merged at read time.
3. **Profile edits ledger** (`profile_edits` table): RLS-forced table tracking all profile overrides (source, status, old/new values) for an undoable list and future chat editor integration.

Artifacts: proposal.md, spec.md (delta), design.md, tasks.md (29 tasks T-267..T-295, all complete), verify-report.md.

## Specs Synced to Canonical

### New Spec: `candidate-profile`

**Path**: `openspec/specs/candidate-profile/spec.md` (created)

**Requirements**:
- `GET /api/me/profile` returns effective (shallow-merged) profile
- `PATCH /api/me/profile` writes override + ledger row atomically
- Manual overrides survive CV re-ingestion
- `POST /api/me/profile-edits/{id}/undo` reverts override and flips ledger status
- `profile_edits` ledger is generic (source/status unconstrained for future reuse)
- `profile_edits` table has FORCE ROW LEVEL SECURITY with tenant isolation policy

This is a **NEW domain** — no prior spec existed.

### Modified Spec: `ingest-cv`

**Path**: `openspec/specs/ingest-cv/spec.md` (updated)

**Change**: Requirement 3 completely rewritten to specify merge behavior and parse-error sanity guard.

**Old Requirement 3** (line 79-116): specified a happy-path parse + parse-error row preservation, but did NOT specify:
- CV merge behavior (was overwriting wholesale)
- Sanity guard to prevent parse errors from destroying good profiles
- Scenario where shorter re-paste should preserve older roles

**New Requirement 3** (6 scenarios):
1. Successful parse — merge-aware happy path with superset guarantee
2. Shorter tailored CV re-paste preserves older role detail
3. Claude parse failure — sanity guard preserves prior good values
4. Anthropic API error — row stays terminal (failed, not stuck)
5. Worker write via tenantQuery (unchanged, documented)
6. Row transitions to processing before Claude call (unchanged, documented)

This merge preserves Requirement 3's spirit (parse guard, row preservation, tenant isolation) while enforcing the new merge-and-superset behavior.

### Modified Spec: `worker-evaluate-job`

**Path**: `openspec/specs/worker-evaluate-job/spec.md` (updated)

**Change**: New Requirement R7 appended (no changes to existing R1–R1.3-Extended).

**New Requirement R7** (1 scenario):
- Evaluation prompt consumes effective profile (merged `profile_json` + `profile_overrides`), not raw `profile_json` alone
- Scenario: manually-overridden target role is reflected in evaluation

This is an **ADDED requirement** — no existing behavior changes, only extension to consume effective profile.

## Verify Report Summary

**Verdict**: PASS

- All 5 test suites (worker, RLS, Go unit, Go integration, web) executed live and passed
- All 7 proposal success criteria verified
- All 29 tasks (T-267..T-295) complete with code present in main
- Spec compliance verified across all 3 domains (candidate-profile, ingest-cv modified, worker-evaluate-job modified)
- 0 CRITICAL issues
- 0 WARNING issues
- 1 non-blocking SUGGESTION (no cross-service end-to-end test for "PATCH override → re-ingest → GET still shows override", though architectural guarantee is code-verified)

Both chained PRs merged to main:
- PR-A #56: DB migration 007 + worker merge/guard/effective-profile
- PR-B #58: Go `profile` package + web `/perfil` page + components

Fresh-context review fixes applied and verified present on main (commits `0dd016a`, `4c477cc`).

## Artifact Inventory

**Archived to**: `openspec/changes/archive/2026-07-12-profile-persistence/`

- [x] proposal.md — intent, scope, approach, risks, rollback
- [x] spec.md — delta spec for 3 domains (candidate-profile NEW, ingest-cv MODIFIED, worker-evaluate-job MODIFIED)
- [x] design.md — 6 architecture decisions (D1–D6), data flows, file changes
- [x] tasks.md — 29 tasks across 7 phases, review workload forecast, 2 chained PRs stacked-to-main
- [x] verify-report.md — test evidence, spec compliance, proposal criteria, issues
- [x] archive-report.md — this file

## SDD Cycle Complete

The profile-persistence change has been:

1. **Proposed** — intent, scope, affected areas, success criteria defined
2. **Specified** — 3 domains with complete scenario-driven requirements
3. **Designed** — 6 architecture decisions, data flows, file manifest
4. **Tasked** — 29 atomic RED/GREEN tasks, 2 chained PR units
5. **Applied** — both PR units implemented, merged to main
6. **Verified** — all tests pass, all spec requirements compliant, zero blockers
7. **Archived** — change folder moved to archive, canonical specs merged, cycle closed

**Ready for next change.** GitHub issue #45 is closed.
