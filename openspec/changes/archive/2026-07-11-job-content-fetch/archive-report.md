# Archive Report: job-content-fetch

**Date Archived**: 2026-07-11  
**Status**: Closed and Verified  
**Verdict**: PASS (2nd pass after closure of Strict TDD test gap)

## Change Summary

**Change Name**: job-content-fetch  
**Shipped**: PR #51, commit `8cc9f7d` (2026-07-08)  
**Scope**: Asynchronous job-content fetching via Playwright for manual and email-ingested jobs, gated by SSRF allowlist.

## What Was Archived

All artifacts for this completed SDD change have been moved to this archive location:

- `proposal.md` — original proposal defining problem, approach, and scope
- `explore.md` — exploration phase findings and recommendation
- `spec.md` — specification of what was actually shipped (retroactive)
- `design.md` — architectural decisions (D1–D6) and data flow
- `tasks.md` — task breakdown (T-1..T-9 shipped + FU-1..FU-7 backlog)
- `verify-report.md` — verification result: PASS (follow-up, 2nd pass)

## Spec Merge Decision

**Action: CREATE** a new canonical spec at `openspec/specs/job-content-fetch/spec.md`.

**Rationale**: This change introduces a new domain (`worker-fetch-job-content`, the 6th pg-boss queue handler) and modifies two existing domains (`jobs-manual-create` with enqueue gate, and `worker-ingest-email` with unconditional enqueue point). The spec is sufficiently distinct from existing specs (`evaluation-input-guards`, `worker-evaluate-job`, etc.) to warrant its own source of truth. The new spec mirrors this archive's `spec.md` exactly for canonical reference by future work.

**Related Specs**:
- `openspec/specs/evaluation-input-guards/spec.md` — documents the 422 guard this change helps clear
- `openspec/specs/worker-evaluate-job/spec.md` — documents the evaluate handler; this change feeds it

## Verification History

### First Pass (PASS-WITH-GAPS)
- All 9 shipped tasks (T-1..T-9) verified against `main` commit `8cc9f7d`.
- All three test suites passed (`go test`, worker `vitest`, web `vitest`).
- **CRITICAL Finding**: Zero automated test coverage for new/modified code paths:
  - `worker/jobs/fetch-job-content.mjs` — untested
  - `worker/shared/fetch-page.mjs` — untested
  - `isHostAllowed` (direct coverage) — untested
  - `ingest-email.mjs` enqueue branch — untested
  - `AddManual`'s enqueue-on-allowlisted-host behavior — untested

This gap was pre-announced and deliberately deferred in `design.md`/`tasks.md` (F-3 / FU-1..FU-4) per original SDD scope decision — test coverage was out of scope for the initial merge.

### Second Pass (PASS, after closure of test gap)
- Commit `2a24a3e` (`test(job-content-fetch): close Strict TDD gap flagged by verify (FU-1..FU-4)`) added:
  - FU-1: `worker/tests/jobs/fetch-job-content.test.mjs` (6 tests)
  - FU-2: `isHostAllowed` coverage in `worker/tests/lib/url-normalize.test.mjs`
  - FU-3: `fetch-job-content` enqueue coverage in `worker/tests/jobs/ingest-email.test.mjs`
  - FU-4: `TestAddManual_EnqueueGate` integration test in `api/internal/jobs/addmanual_enqueue_integration_test.go`
- Re-verification ran live test suites: Worker 201/205 passed, Go 15/15 packages, Web 52/52.
- All 13 new tests confirmed live execution (not skipped or mocked).
- Spot-check confirmed substantive assertions (not tautologies, ghost loops, or smoke tests).
- Spec compliance improved to 10/11 scenarios COMPLIANT; sole PARTIAL (`fetchPageText`'s own body) is a narrow, branchless-code, low-risk gap, downgraded to WARNING.

**Verdict**: PASS — CRITICAL gap closed, FU-5/FU-6/FU-7 remain open as explicitly out-of-scope backlog.

## Outstanding Backlog (Not Blockers)

Per `tasks.md`, the following items remain unchecked as explicitly out-of-scope:

- **FU-5 (F-1)** — De-duplicate the SSRF allowlist currently implemented independently in JS and Go (no shared source of truth; hand-synced).
- **FU-6 (F-2)** — Remove or wire the dead `UpdateJobScrapedContent` sqlc query (regenerated but unwired in API).
- **FU-7 (O-1)** — Add monitoring/alerting for combined Chromium memory pressure from `generate-pdf` and `fetch-job-content` sharing `shm_size:1gb`.

These are tracked in `tasks.md` and `design.md` and do NOT block closure of this change.

## Implementation Footnotes

### Deviations from Original Proposal (See spec.md)

1. **`UpdateJobScrapedContent` sqlc query is dead code** — added to `db/queries/jobs.sql` and regenerated, but never wired in any Go handler (worker writes via raw inline SQL through `tenantQuery`, not via sqlc).

2. **Two independent SSRF gates** — `HOST_RULES`/`isHostAllowed` (JS) and `allowedHostPatterns`/`lookupAllowedHost` (Go) are hand-synced with no shared source of truth. End state is equivalent (no Playwright navigation to disallowed hosts either path), but inconsistent gatekeeping location.

3. **Asymmetric enqueue-time gating** — `AddManual` (Go) checks host allowlist BEFORE enqueueing; `ingest-email` (worker) enqueues unconditionally and relies on worker handler's own `isHostAllowed` check at consume time.

4. **No automated test coverage initially** — Fixed retroactively in commit `2a24a3e` via FU-1..FU-4.

5. **Combined Chromium memory pressure untested** — `generate-pdf` and `fetch-job-content` both at `teamSize:3` share `shm_size:1gb` (`docker-compose.yml`); operational risk flagged but not mitigated in code.

## Files Written to Archive

```
openspec/changes/archive/2026-07-11-job-content-fetch/
├── proposal.md               (from source)
├── explore.md                (from source)
├── spec.md                   (from source, also copied to openspec/specs/job-content-fetch/spec.md)
├── design.md                 (from source)
├── tasks.md                  (from source)
├── verify-report.md          (from source)
└── archive-report.md         (this file)
```

## Canonical Spec Location

- **Primary**: `openspec/specs/job-content-fetch/spec.md` (created during archiving)
- **Archive**: `openspec/changes/archive/2026-07-11-job-content-fetch/spec.md` (historical reference)

## SDD Cycle Complete

The `job-content-fetch` change has completed the full SDD cycle:
1. ✅ **Exploration** — identified approaches, recommended Playwright + SSRF gate
2. ✅ **Proposal** — defined scope, approach, dependencies, risks
3. ✅ **Spec** — documented actual implementation (retroactive)
4. ✅ **Design** — recorded architecture decisions (D1–D6) and deviations
5. ✅ **Tasks** — listed 9 shipped tasks + 7 backlog items
6. ✅ **Apply** — shipped in PR #51, merged to `main` (commit `8cc9f7d`)
7. ✅ **Verify** — verified twice, PASS after closure of test gap
8. ✅ **Archive** — this report, artifacts preserved, original folder removed

The change is **closed** and ready for the next SDD item.
