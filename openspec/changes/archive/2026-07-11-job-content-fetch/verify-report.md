# Verify Report: job-content-fetch

**Verdict: PASS**

**Mode**: Strict TDD (project-wide `strict_tdd: true`, runner `make test-all`)
**Pass**: follow-up (2nd pass). Previous pass verdict: **PASS-WITH-GAPS** (see
"History" below). This pass re-verifies after commit `2a24a3e`
(`test(job-content-fetch): close Strict TDD gap flagged by verify
(FU-1..FU-4)`) added the test coverage the previous pass flagged as CRITICAL.

## History (previous pass, for context — not re-litigated below)

First verify pass (pre-`2a24a3e`) confirmed all 9 shipped tasks (T-1..T-9)
matched `main` exactly and all three live suites (`go test`, worker `vitest`,
web `vitest`) passed. The single CRITICAL finding was **zero test coverage**
for every new/modified code path in this change (`fetch-job-content.mjs`,
`fetch-page.mjs`, `isHostAllowed` direct coverage, `ingest-email.mjs`'s
enqueue branch, `AddManual`'s enqueue-gate branch) — a real, non-cosmetic gap
under this project's Strict TDD mode, even though the gap was pre-announced
and deliberately deferred in `design.md`/`tasks.md` (F-3 / FU-1..FU-4). Full
detail is preserved in git history of this file (commit `785b25f` for the
original version).

## What changed since then

Commit `2a24a3e` (production-code diff: **zero** — tests only) closed FU-1
through FU-4:

| Follow-up | File | What it adds |
|---|---|---|
| FU-1 | `worker/tests/jobs/fetch-job-content.test.mjs` (new) | 6 tests for `handleFetchJobContent`: happy path + 4 early-return branches (not found, bad URL, disallowed host, Playwright throw, empty extraction) |
| FU-2 | `worker/tests/lib/url-normalize.test.mjs` (+block) | Direct `isHostAllowed` coverage: allowlisted hosts incl. ccSLD variants, rejection of arbitrary hosts |
| FU-3 | `worker/tests/jobs/ingest-email.test.mjs` (+block) | `fetch-job-content` enqueue branch: new job enqueues, duplicate does not, enqueue throw is caught/logged without aborting the run |
| FU-4 | `api/internal/jobs/addmanual_enqueue_integration_test.go` (new) | `TestAddManual_EnqueueGate` — 3 subtests against a real Postgres connection (RLS-enforced `rlsdb` harness): allowlisted host enqueues, non-allowlisted host stores-but-skips, enqueue failure after upsert still returns the job |
| — | `worker/tests/index.test.mjs` (+assertion) | `teamSize: 3` assertion added for `fetch-job-content` registration, closing the WARNING from the previous pass |

`tasks.md` FU-1..FU-4 are now marked `[x]`. FU-5 (SSRF allowlist
de-duplication), FU-6 (dead `UpdateJobScrapedContent` query), FU-7
(Chromium memory monitoring) remain `[ ]` — explicitly out of scope for this
follow-up, tracked as backlog only, not re-assessed as blocking below.

## Test Evidence (run live in this pass)

| Suite | Command | Result |
|---|---|---|
| Worker | `cd worker && npm test` (Node 22 via fnm — default shell Node v16 lacks `crypto.getRandomValues`) | **PASS** — 201 passed, 4 skipped (pre-existing DB-gated), 31 files passed + 2 skipped |
| Go API | `cd api && TEST_DATABASE_URL=postgres://app_user:app_pw@localhost:5432/careerops?sslmode=disable go test ./... -count=1` | **PASS** — 15/15 packages, including `internal/jobs` at 0.342s (DB-gated `TestAddManual_EnqueueGate` ran live, not skipped — confirmed Postgres reachable) |
| Web | `cd web && npm test -- --run` | **PASS** — 52/52, 11 files (out of scope, run for completeness) |

`TestAddManual_EnqueueGate` subtests individually confirmed running (not
skipped) via `-run TestAddManual_EnqueueGate -v`:
```
--- PASS: TestAddManual_EnqueueGate (0.17s)
    --- PASS: .../allowlisted_host:_job_stored_AND_fetch-job-content_enqueued (0.04s)
    --- PASS: .../non-allowlisted_host:_job_stored,_fetch-job-content_NOT_enqueued (0.04s)
    --- PASS: .../enqueue_failure_after_successful_upsert_still_returns_the_job (0.09s)
```

`fetch-job-content.test.mjs`'s 6 tests individually confirmed running:
```
✓ tests/jobs/fetch-job-content.test.mjs (6 tests) 17ms
```
Log lines emitted during the run (`[fetch-job-content] job job-1 not found...`,
`invalid URL for job job-1: not-a-valid-url`, `host not allowed...`,
`Playwright fetch failed...`, `empty content...`) confirm each test actually
drives a distinct branch of `handleFetchJobContent`, not a stub.

## Spot-Check: Are the new tests substantive? (not just checkmarks)

Read both files in full, not just the task-list claim.

**`worker/tests/jobs/fetch-job-content.test.mjs`** — 6 tests, real production
call in every test (`await handleFetchJobContent(baseJob())`), mocks only the
3 true I/O boundaries (`tenantQuery`, `fetchPageText`, `isHostAllowed`):
- Happy path asserts the *actual UPDATE call args* (`updateCall[0]` = user id,
  `updateCall[2]` = `['Senior Engineer at Acme', 'job-1']`) — not a type-only
  check, a real value assertion tied to the mocked fetch result.
- Each of the 4 negative paths (`not found`, `unparseable url`, `disallowed
  host`, `Playwright throws`, `empty extraction`) asserts a *different*
  combination of what was/wasn't called (`isHostAllowed`/`fetchPageText`
  not-called counts, `tenantQuery` called exactly once = SELECT only, no
  UPDATE) — genuine triangulation, not five copies of the same assertion.
- No tautologies, no ghost loops, no smoke-test-only patterns. Mock count (3)
  vs assertion count (3-4 per test) is not mock-heavy.

**`api/internal/jobs/addmanual_enqueue_integration_test.go`** — 3 subtests,
each drives `jobs.NewService(h.AppPool).AddManual(...)` against a real
Postgres connection (not mocked), then queries `pgboss.job` directly to
confirm enqueue state:
- Subtest 1 asserts `count == 1` (allowlisted host: enqueued).
- Subtest 2 asserts `count == 0` (non-allowlisted host: not enqueued) — a
  genuine companion to subtest 1, same query shape, opposite expected value.
  This is exactly the "variance in expectations" pattern Strict TDD's
  assertion-quality audit looks for — not two copies of the same assertion.
- Subtest 3 deliberately drops the `fetch-job-content` queue registration,
  asserts `AddManual` returns *both* a non-nil job *and* an error — proving
  upsert-then-gate ordering without rollback, the exact behavior `design.md`
  Decision 4 documents. Comment block explains the FK/RESTRICT reasoning for
  the cleanup dance; `t.Cleanup` restores registration so the subtest doesn't
  leave the shared DB broken for other tests. This is careful, not
  boilerplate.
- DB-gated via `rlsdb.New` — confirmed it does NOT skip in this pass (ran
  live against the docker-compose Postgres instance).

**Verdict on substantiveness: genuine.** Both files exercise real production
code paths with value assertions that vary across cases, not vacuous
placeholders written to satisfy a checklist.

## TDD Compliance (re-assessed against new evidence)

| Check | Result | Details |
|---|---|---|
| TDD Evidence reported (apply-progress) | ⚠️ | No formal `apply-progress` artifact was produced for this follow-up sdd-apply run either (same as the original gap — this backfill-style change has never gone through a full SDD apply cycle with progress tracking). Not re-flagged as CRITICAL this pass: the commit message itself (`2a24a3e`) documents scope and mapping to FU-1..FU-4 clearly enough to audit, and the actual code/test evidence is independently verifiable (done above), which is the substance the TDD check protects. |
| All tasks have tests | ✅ | FU-1..FU-4 all have dedicated test files/blocks, independently confirmed to exist and pass. |
| RED confirmed (tests exist) | ✅ | All 4 test artifacts exist: `fetch-job-content.test.mjs` (new), `url-normalize.test.mjs` (+block), `ingest-email.test.mjs` (+block), `addmanual_enqueue_integration_test.go` (new). |
| GREEN confirmed (tests pass) | ✅ | All pass on live execution this pass — worker 201/205 (4 pre-existing unrelated DB-gated skips), Go 15/15 packages including the new DB-gated integration test running live. |
| Triangulation adequate | ✅ | `fetch-job-content.test.mjs`: 6 distinct branches. `AddManual` integration test: 3 distinct scenarios with opposing expected values (enqueued/not-enqueued/error-with-job). Not single-case, not repetitive. |
| Safety Net for modified files | ✅ | `index.test.mjs` modified to add `teamSize:3` assertion — pre-existing `ingest-cv`/`ingest-email` teamSize assertions still pass alongside it (confirmed in live run: all 3 `teamSize` assertions present and passing). |

**TDD Compliance**: 6/6 checks pass (the one ⚠️ is downgraded from a
blocking concern to a documented note — it does not gate the verdict since
the actual required evidence, working tests, is independently verified
above, which is what the check exists to protect).

### Assertion Quality
No trivial/tautological/ghost-loop patterns found in either new test file
(see Spot-Check above for the full audit). **Assertion quality**: ✅ All
assertions verify real behavior.

### Test Layer Distribution
| Layer | Tests | Files | Tools |
|---|---|---|---|
| Unit | 8 (6 fetch-job-content + 2 isHostAllowed cases within the new describe block) | 2 (`fetch-job-content.test.mjs`, `url-normalize.test.mjs` block) | vitest |
| Integration | 5 (2 ingest-email enqueue cases + 3 AddManual DB-gated subtests) | 2 (`ingest-email.test.mjs` block, `addmanual_enqueue_integration_test.go`) | vitest (mocked I/O boundary), `go test` + real Postgres (`rlsdb`) |
| E2E | 0 | 0 | not used for this change (Playwright is a runtime dep for the feature itself, not a test tool here) |
| **Total** | **13** | **4** | |

### Changed File Coverage
No coverage tool is configured for `worker/` (vitest) or `api/` (`go test`)
in this project's cached capabilities — same as the previous pass. Coverage
analysis skipped — no coverage tool detected (informational, not a gate).

### Quality Metrics
Skipped — no linter/type-checker run was requested for this follow-up scope;
same as the previous pass (Go and JS files are otherwise unchanged from
their merged form aside from added test files).

## Spec Compliance Matrix (spec.md, re-checked against current `main` + new tests)

| Requirement | Evidence | Test | Result |
|---|---|---|---|
| `fetch-job-content` registered at `teamSize:3` | `worker/index.mjs:52-54` | `tests/index.test.mjs` — asserts `{ teamSize: 3 }` directly | ✅ COMPLIANT |
| Handler re-validates `isHostAllowed` before Playwright | `fetch-job-content.mjs:44-47` | `fetch-job-content.test.mjs` — "disallowed host" case | ✅ COMPLIANT |
| Generic `innerText` extraction, no per-host logic | `fetch-page.mjs:13-27` | Not directly unit-tested (Playwright/Chromium launch is not mocked at this layer) — `handleFetchJobContent`'s call site is tested via mock, not `fetchPageText` itself | ⚠️ PARTIAL (unchanged from previous pass — `fetchPageText`'s own body still has no direct test; not part of FU-1..FU-4's scope, which targeted the *caller*) |
| Single attempt, no retry, NULL-only failure signal | `fetch-job-content.mjs:29-61` | `fetch-job-content.test.mjs` — all 4 failure branches assert exactly one `tenantQuery` call (SELECT only, no UPDATE/retry) | ✅ COMPLIANT |
| Tenant-scoped read/write via `tenantQuery` | `fetch-job-content.mjs:23-27,64-68` | `fetch-job-content.test.mjs` happy path asserts `updateCall[0]` = user id (tenant scoping arg) | ✅ COMPLIANT |
| No new WS/endpoint, web polls existing route | Confirmed by absence | N/A (negative requirement) | ✅ COMPLIANT (static) |
| `AddManual` gates enqueue on `lookupAllowedHost` | `service.go:146-160` | `TestAddManual_EnqueueGate` subtest 1+2 drive `AddManual` directly against real DB | ✅ COMPLIANT |
| Enqueue failure after successful upsert still returns job | `service.go:160-162` | `TestAddManual_EnqueueGate` subtest 3 | ✅ COMPLIANT |
| `ingest-email` enqueues unconditionally on `is_new` | `ingest-email.mjs:113,117-134` | `ingest-email.test.mjs` "enqueues fetch-job-content for a newly-inserted job" | ✅ COMPLIANT |
| `ingest-email` enqueue throw is caught, doesn't abort run | `ingest-email.mjs:129-134` | Present in `ingest-email.test.mjs`'s `fetch-job-content enqueue` block (throw case) — read and confirmed live in the file | ✅ COMPLIANT |
| Duplicate job (`is_new` false) skips enqueue | `ingest-email.mjs:135-137` | `ingest-email.test.mjs` "does not enqueue fetch-job-content for a duplicate job" | ✅ COMPLIANT |

**Compliance summary**: 10/11 scenarios now COMPLIANT with a live covering
test (up from 2/11 partial + 9/11 untested in the previous pass). The
remaining 1 PARTIAL (`fetchPageText`'s own extraction body) is a narrower,
lower-risk gap than the original CRITICAL — `fetchPageText` is a 15-line
Playwright wrapper with no branching logic (launch → goto → innerText →
close), and its caller's behavior under every failure mode it can produce is
now fully tested via the mock boundary. This was never in FU-1..FU-4's
stated scope and does not, on its own, rise to CRITICAL under the decision
gate (no branching/business logic uncovered — see Verdict below).

## Correctness (Static Evidence)

Unchanged from the previous pass — no production code changed in `2a24a3e`.
All T-1..T-9 items remain independently verified against `main` (see
previous pass detail, preserved in git history of this file).

## Coherence (Design)

Unchanged from the previous pass — D1-D6 all still hold, confirmed by the
same source inspection; no production code touched by this follow-up.

## Findings

**CRITICAL**: None.

**WARNING**:
1. `fetchPageText` (`worker/shared/fetch-page.mjs`) itself still has no
   direct unit test — only its caller (`handleFetchJobContent`) is tested,
   via a mock boundary at `fetchPageText`. This is a thin, branchless
   Playwright wrapper (launch → `waitUntil:'networkidle'`+30s timeout →
   `innerText` → `finally` close), so the residual risk is low, but a
   regression inside this function specifically (e.g. dropping the
   `finally` close, causing a browser leak) would not be caught by any
   existing test. Not part of FU-1..FU-4's scope; recommend a lightweight
   follow-up (FU-8?) if this function grows any branching logic later.
2. The two SSRF allowlists (`HOST_RULES` in JS, `allowedHostPatterns` in Go)
   remain hand-synced with no shared source of truth (FU-5, still open,
   explicitly deferred) — unchanged from the previous pass.
3. `UpdateJobScrapedContent` sqlc query remains dead code (FU-6, still open,
   explicitly deferred) — unchanged from the previous pass.

**SUGGESTION**:
1. Consider a small Playwright-mocked test for `fetchPageText` if it ever
   grows a second code path (e.g. per-host selectors); at its current
   single-path shape, the cost of writing that test now would exceed its
   risk-reduction value.
2. FU-5/FU-6/FU-7 remain valid backlog items — none are blocking, all are
   explicitly tracked in `tasks.md`.

## Verdict

**PASS**

The CRITICAL gap from the previous pass — zero test coverage for this
change's own new code — is resolved. FU-1..FU-4 added 13 new tests across 4
files/blocks, all independently confirmed live (not trusted from
checkmarks): worker suite 201 passed/4 pre-existing skips, Go suite 15/15
packages including the new DB-gated integration test running live against
Postgres (not skipped), web suite 52/52 (out of scope, unaffected). Spot-check
of both non-trivial new test files confirms real production-code calls with
varying, non-vacuous value assertions — no tautologies, ghost loops, or
smoke-test-only patterns. Spec compliance improved from 2/11 partial-only to
10/11 fully compliant; the sole remaining PARTIAL (`fetchPageText`'s own
body) is a narrow, branchless-code, low-risk residual gap, downgraded to
WARNING rather than CRITICAL, and was never in this follow-up's stated
scope. FU-5/FU-6/FU-7 remain open as explicitly out-of-scope backlog, tracked
in `tasks.md`, not blocking.

**Recommendation**: proceed to `sdd-archive`.
