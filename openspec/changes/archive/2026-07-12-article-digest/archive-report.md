# Archive Report: article-digest

**Status**: ARCHIVED  
**Date**: 2026-07-12  
**Change**: article-digest (slice #2 of `candidate-profile-kb` exploration)  
**Verdict**: PASS (all verification criteria met)

## Executive Summary

The `article-digest` change has been completed, verified, and archived. It introduces a new user-facing table for storing per-project proof-point entries (title + markdown body), with automatic injection into job evaluations as a bounded, cached prompt block. The change ships across three chained PRs (PR-A #57 DB, PR-B #60 Go API, PR-C #61 worker+web), all merged to `main`. Canonical specs have been created and updated. The change closes slice #2 of the `candidate-profile-kb` exploration (slice #1, `profile-persistence`, was archived independently on 2026-07-12).

## Artifacts Archived

All artifacts from `openspec/changes/article-digest/` have been copied to `openspec/changes/archive/2026-07-12-article-digest/`:

- **proposal.md** — Intent, scope, approach, risks, rollback plan, success criteria
- **spec.md** — Formalized requirements and scenarios for both `article-digest` (NEW) and `worker-evaluate-job` (MODIFIED)
- **design.md** — Technical approach, 6 architecture decisions, file changes, open questions, readiness confirmation
- **tasks.md** — 18 tasks (T-296..T-313) across 6 phases, review workload forecast, chained PR strategy
- **verify-report.md** — PASS verdict; all 18 tasks verified complete; test evidence (pgTAP, Go, worker, web); spec compliance matrix; zero issues (CRITICAL, WARNING, or SUGGESTION)

## Canonical Spec Merge Decisions

### 1. New Canonical Spec: `openspec/specs/article-digest/spec.md` (CREATED)

A new canonical capability spec was created at `openspec/specs/article-digest/spec.md` containing all article-digest domain requirements from the change spec:

- Requirement: Create a digest entry scoped to the authenticated user
- Requirement: List returns only the current user's entries, newest first
- Requirement: Delete removes exactly one owned entry, scoped by user
- Requirement: Row-level security enforces tenant isolation on `article_digests`

**Merge action**: Copy — the article-digest change is NEW, so its spec is copied directly as the canonical spec with zero modification.

### 2. Existing Canonical Spec: `openspec/specs/worker-evaluate-job/spec.md` (MODIFIED)

The change introduced a new cached prompt block requirement for the worker-evaluate-job capability. This requirement was merged into the existing canonical spec as **Requirement R8** (following the existing R1–R7):

**R8**: A third cached system block carries the user's article digests, bounded and ordered newest-first
- N=20 entries cap
- ~24,000-character ceiling
- `cache_control: { type: 'ephemeral' }` treatment
- Appended after existing static-prompt and CV+profile blocks

**R8-Extended**: The digest block is omitted entirely when the user has zero entries

**Merge action**: Append as R8 — the new requirement does not modify or replace any existing R1–R7 requirements; it adds a new capability that was not previously specified, consistent with the project's convention of accumulating requirements as new capabilities are layered.

## Completeness Checklist

- [x] All 5 change artifacts (proposal, spec, design, tasks, verify-report) copied to archive folder
- [x] Canonical specs updated: new `article-digest` created, `worker-evaluate-job` merged with R8/R8-Extended
- [x] Archive folder named with ISO date: `2026-07-12-article-digest`
- [x] Archive folder contains all required artifacts with no gaps
- [x] Verification verdict: PASS (0 CRITICAL, 0 WARNING, 0 SUGGESTION)
- [x] All 18 tasks (T-296..T-313) confirmed complete and code-verified
- [x] All test suites passed live (pgTAP, Go unit, Go integration, worker, web)
- [x] Chained delivery as designed: PR-A (DB + RLS test) → PR-B (Go API) → PR-C (worker + web)
- [x] All PRs merged to `main` (commit hashes verified in verify-report.md)

## Source of Truth Updates

The following canonical specs now reflect the changes introduced by article-digest:

| Spec | Changes |
|------|---------|
| `openspec/specs/article-digest/spec.md` | NEW — full spec for article-digest CRUD capability |
| `openspec/specs/worker-evaluate-job/spec.md` | MODIFIED — added R8 (third cached prompt block) and R8-Extended (omit block when empty) |

## Exploration Context

This change is **slice #2** of the `candidate-profile-kb` exploration (see `openspec/changes/candidate-profile-kb/explore.md`, Part 4):

- **Slice #1**: `profile-persistence` (archived 2026-07-12) — profile overrides and merges
- **Slice #2**: `article-digest` (this change, archived 2026-07-12) — proof-point entries and evaluation enrichment

The two slices are independent; no dependency in either direction. Both landed in the same window and may be merged in any order to `main`.

## Risks and Mitigations

**None noted in verification.** All risks flagged in the proposal were either:
- Resolved by design (migration numbering, truncation algorithm, empty-state handling)
- Mitigated by implementation (N=20 / 24 KB ceilings, RLS from day one)
- Validated by testing (all four prompt-truncation scenarios pass)

## SDD Cycle Completion

The article-digest change has completed the full SDD cycle:

1. ✅ **Proposal** (approved) — scope, approach, success criteria
2. ✅ **Spec** (approved) — requirements and scenarios
3. ✅ **Design** (approved) — technical decisions, file changes, exact SQL/Go signatures
4. ✅ **Tasks** (forecast) — 18 tasks, chained PR strategy, review workload
5. ✅ **Apply** (completed) — all tasks done, 3 PRs merged to main
6. ✅ **Verify** (PASS) — all spec requirements verified against real source, all tests green
7. ✅ **Archive** (this report) — artifacts moved, canonical specs updated

Ready for the next change in the exploration or a new independent proposal.

## Notes for Next Session

- Original `openspec/changes/article-digest/` folder must be deleted by the orchestrator (this executor cannot perform file system deletions). The archive folder at `openspec/changes/archive/2026-07-12-article-digest/` is complete and ready as the source of truth.
- The new `openspec/specs/article-digest/spec.md` and updated `openspec/specs/worker-evaluate-job/spec.md` are now the canonical source for these capabilities.
- If `profile-persistence` (slice #1) has not yet landed or been archived when the next session begins, coordinate the two slices' migration numbers (007 vs 008) before apply.
