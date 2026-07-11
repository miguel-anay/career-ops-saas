# Archive Report: Evaluation Quality

**Change**: evaluation-quality  
**Date Archived**: 2026-07-11  
**Status**: ARCHIVED — Complete, verified, merged to main

## What Was Archived

The **evaluation-quality** SDD change has been fully implemented, verified with a PASS verdict, and is now archived. This change addresses token-burning evaluation requests caused by missing input (CV or job description).

### Scope Delivered

- **Input guards** (`ErrCVMissing` / `ErrJobContentMissing` → 422): block evaluation with 0 tokens when CV or scraped JD is absent
- **`blocks_json` array fix**: persist as array so the web client renders collapsible blocks
- **Web 422 UX**: distinguish CV-missing and JD-unavailable states on the job-detail page
- **Prompt enrichment** (zero new deps): pass `job.received_at` age + STAR-mapping + negotiation guidance text

### Not Included (Future Change)

- **`fetch-job-content` (Playwright JD scraping)** — deferred to separate change `job-content-fetch`. This change's `ErrJobContentMissing` guard is the gate that future slice will satisfy.

## Implementation Summary

### Chained PRs Delivered

| PR | Branch | Scope | Status |
|---|---|---|---|
| #49 | `feat/48-evaluation-guards` | Go guards + worker parser/prompt | Merged |
| #50 | `feat/48-evaluation-web` | Web 422 surfacing + array render | Merged |

Both PRs target `main`; issue #48 closed.

### Test Evidence

All test suites pass:
- **Go** (`api/`): 14/14 packages
- **Worker** (`worker/`): 172/176 tests (4 pre-existing DB-gated skips)
- **Web** (`web/`): 53/53 tests
- **RLS integration**: passed via combined test run

Task T-266 (full `make test-all`) executed live in verification phase and passed.

## Specs Merged

Three canonical specs updated to reflect the new behavior:

### 1. NEW: `openspec/specs/evaluation-input-guards/spec.md`

Created new capability domain documenting:
- Requirement: Block evaluation when CV is missing (422 `cv_missing`)
- Requirement: Block evaluation when job content is missing (422 `job_content_missing`)
- Requirement: Existing error precedence preserved (404/402 paths unchanged)

### 2. UPDATED: `openspec/specs/worker-evaluate-job/spec.md`

Added two new requirements to the existing DDD-refactored evaluate-job spec:
- **R1.2-Extended**: `blocks_json` persisted as an array (was object keyed by block letter)
  - Scenarios: new evaluation, re-evaluation, parse-error handling, array safety
- **R1.3-Extended**: Prompt includes posting-age signal and STAR/negotiation guidance
  - Scenarios: posting age rendered, STAR-mapping + negotiation text, 7-block schema preserved

### 3. UPDATED: `openspec/specs/web-frontend-structure/spec.md`

Added two new requirements to the existing feature-folders spec:
- **Requirement**: Job-detail page renders CV-missing and JD-unavailable states
  - Distinct panels for each 422 code, actionable copy, future issue #45 link structure
- **Requirement**: Job-detail page renders array-shaped `blocks_json`
  - 7 collapsible sections now render, backward-compat with pre-existing object-shaped rows

## Change Artifacts Moved

All SDD artifacts for this change have been moved to:

```
openspec/changes/archive/2026-07-11-evaluation-quality/
├── proposal.md              (intent, scope, capabilities, risks, rollback)
├── spec.md                  (delta specs: evaluation-input-guards, worker-evaluate-job mods, web-frontend-structure mods)
├── design.md                (technical approach, architecture decisions)
├── tasks.md                 (15 tasks, all marked [x] complete)
├── apply-progress.md        (implementation checkpoints, PR references)
├── verify-report.md         (PASS verdict with full test evidence)
└── archive-report.md        (this file)
```

No `specs/` subdirectory (no standalone delta spec files) — delta spec was delivered as inline sections within `spec.md`.

## Verification Summary

**Verdict**: **PASS**

### Spec Compliance (per verify-report.md)

All requirements met:
- ✅ 422 `cv_missing` returned before enqueue with zero token spend
- ✅ 422 `job_content_missing` returned for manual/email jobs
- ✅ 404/402 precedence preserved (ownership and usage-limit checks unaffected)
- ✅ `blocks_json` persisted as array (`[{label, content}, ...]`)
- ✅ Parse-error path yields array-safe blocks (`[]` fallback)
- ✅ Prompt includes posting-age age + STAR/negotiation guidance; 7-block schema unchanged
- ✅ Web 422 panels distinct and actionable
- ✅ Array `blocks_json` renders 7 collapsible blocks; legacy object-shaped rows degrade safely

### Tasks Completion

All 15 tasks marked `[x]` complete:
- T-252–T-255: Go content guards (4 tasks)
- T-256–T-261: Worker parser + prompt (6 tasks)
- T-262–T-265: Web 422 surfacing + array render (4 tasks)
- T-266: Cross-cutting verification (`make test-all` green)

### Known Issues Surfaced (not change bugs)

**WARNING** (pre-existing, highlighted by this change):
1. `rlsdb` integration-test fixtures don't reset the `usage` table between runs against a persistent dev DB, so tests using fixed test emails (e.g., `TestEvaluateRLS_Integration`) can flake after ~5 cumulative runs. Recommend follow-up: `DELETE FROM usage` in test teardown per run/month.
2. Running worker/web tests requires Node ≥18 (`crypto.getRandomValues`); default shell had v16.16 and crashed Vitest silently. Recommend `.nvmrc` or engines-check note.

Both are pre-existing infrastructure issues, not caused by this change.

## Risks Mitigated

| Risk | Mitigation | Status |
|---|---|---|
| Guard blocks legitimate jobs (ATS jobs with empty scraped_content) | Guard on CV always; JD guard only when NULL/empty — ATS jobs populate scraped_content | ✅ Verified by test coverage |
| PR size >400 lines | Chained: PR-A (Go+worker ~240 lines) + PR-B (web ~220 lines) | ✅ Delivered as two PRs |
| `blocks_json` shape change breaks old rows | Web guard `Array.isArray` already present, tolerates objects | ✅ Backward-compat confirmed |
| SSRF on arbitrary URLs (Playwright future work) | Out of scope; owned by `job-content-fetch` change | ✅ Deferred explicitly |

## Capabilities Added/Modified

### New Capabilities
- `evaluation-input-guards`: API rejects evaluation with 422 + actionable message when CV or JD content is missing, before any enqueue/LLM call.

### Modified Capabilities
- `worker-evaluate-job`: Blocks persisted as array; prompt gains posting-age + STAR/negotiation text (A-G schema preserved).
- `web-frontend-structure`: Job-detail page renders CV-missing / JD-unavailable 422 states and array-shaped A-G blocks.

## Files Changed

### Go API
- `api/internal/evaluate/service.go` — CV/JD guards + typed errors
- `api/internal/evaluate/handler.go` — 422 mapping
- `api/internal/evaluate/service_test.go` — guard tests

### Worker
- `worker/adapters/PgEvaluationRepository.mjs` — `blocks_json` → array
- `worker/lib/prompt.mjs` — posting-age + STAR/negotiation text
- `worker/domain/EvaluationParser.mjs` — array emit (A→G sorted)
- `worker/tests/domain/evaluation-parser.characterization.test.mjs` — array assertions
- `worker/tests/adapters/pg-evaluation-repository.test.mjs` — array shape test

### Web
- `web/app/jobs/[id]/page.tsx` — 422 state handling, array render confirmation
- `web/__tests__/jobs.test.tsx` — 422 code assertions
- `web/lib/api.ts` — `ApiError{status, code}` class

## SDD Cycle Complete

✅ **Proposed** → Scope, approach, capabilities defined  
✅ **Specified** → Three domains (new + two modified) with full requirements  
✅ **Designed** → Technical approach, architecture decisions documented  
✅ **Tasked** → 15 tasks defined, grouped by work unit, delivery split planned  
✅ **Applied** → All tasks completed, PRs #49 and #50 merged to main  
✅ **Verified** → PASS: all spec requirements met, test suites green, issue #48 closed  
✅ **Archived** → Change folder moved, delta specs merged, archive report persisted  

## Next Steps

The change is now fully archived and closed. Team can proceed with:
1. Future `job-content-fetch` SDD change (uses this change's `ErrJobContentMissing` guard as gate)
2. Issue #45 (CV visibility UI) — the 422 CV-missing state is structured to deep-link to this
3. Address the two pre-existing warnings (rlsdb test isolation, Node version docs) in a follow-up

---

**Archive metadata**:
- Change name: `evaluation-quality`
- Archival date: 2026-07-11
- SDD phases completed: propose → spec → design → tasks → apply → verify → archive
- Test verdict: PASS (all test suites green)
- GitHub status: Issue #48 closed; PRs #49, #50 merged to main
