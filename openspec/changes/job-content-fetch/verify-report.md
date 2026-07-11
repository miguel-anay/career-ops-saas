# Verify Report: job-content-fetch

**Verdict: PASS-WITH-GAPS**

**Mode**: Strict TDD (project-wide `strict_tdd: true`, runner `make test-all`)

Shipped to `main` via PR #51 (`feat/49-job-content-fetch`, commit `8cc9f7d`),
already merged. `spec.md`/`design.md`/`tasks.md` were backfilled retroactively
*after* merge (commit `088c523`). This is the first verify pass — no
`apply-progress` artifact exists (none was ever produced, since no `sdd-apply`
phase ran before the code shipped).

## Tasks (tasks.md) vs Code State — no corrections needed

All 9 checked tasks (T-1..T-9) were independently re-verified against the
current `main` tree, not trusted from the backfilled checkmarks:

| Task | Claim | Verified in code |
|---|---|---|
| T-1 | `fetch-page.mjs` → `fetchPageText` | `worker/shared/fetch-page.mjs:13-27` — Chromium launch, `networkidle`+30s timeout, `innerText`, `finally` close. Matches exactly. |
| T-2 | `fetch-job-content.mjs` → `handleFetchJobContent` | `worker/jobs/fetch-job-content.mjs:19-71` — tenant read → URL parse → `isHostAllowed` → `fetchPageText` → tenant write; every failure path logs+returns, no throw/retry. Matches exactly. |
| T-3 | Register at `teamSize:3` | `worker/index.mjs:52-54` — `registerWorker('fetch-job-content', handleFetchJobContent, { teamSize: 3 })`. Matches. |
| T-4 | 6th `QUEUE_NAMES` entry | `worker/scripts/install-pgboss.mjs:39` — `fetch-job-content` present as 6th entry. Matches. |
| T-5 | `isHostAllowed` export | `worker/lib/url-normalize.mjs:37-39` — exported, reuses `HOST_RULES`. Matches. |
| T-6 | `allowedHostPatterns`/`lookupAllowedHost` + `AddManual` gate | `api/internal/jobs/service.go:79-104,146-160` — gate runs after unconditional upsert, before enqueue. Matches. |
| T-7 | `UpdateJobScrapedContent` sqlc query, unwired | `db/queries/jobs.sql:60-64` + `api/internal/db/jobs.sql.go:176-205` — generated correctly; confirmed **zero callers** in `api/` via grep. Matches "shipped but dead" claim exactly. |
| T-8 | `ingest-email.mjs` unconditional enqueue on `is_new` | `worker/jobs/ingest-email.mjs:113,117-134` — `xmax = 0 AS is_new`, `boss.send` inside `try/catch`, catch logs and does not abort the loop. Matches. |
| T-9 | `make test-all` passed at merge time | Re-run live in this pass (below) — passes today too. |

**No tasks.md edits were made.** The backfill's checkbox state was accurate
for T-1..T-9. The 7 unchecked Follow-ups (FU-1..FU-7) were also independently
re-checked and are genuinely still unimplemented (see Test Evidence and
Findings below) — no corrections needed there either.

## Test Evidence (run live in this pass)

| Suite | Result | Notes |
|---|---|---|
| `cd api && go test ./... -count=1` | **PASS** — 15/15 packages | Includes `internal/jobs` (`TestDetectPlatform`, `TestLookupAllowedHost`). |
| `cd worker && npm test` (Node 22 via fnm, default shell Node v16 lacks `crypto.getRandomValues`) | **PASS** — 172/176 (4 pre-existing DB-gated skips), 32 files | `tests/index.test.mjs` confirms `fetch-job-content` handler registration boots cleanly, but asserts nothing about its `teamSize` (only `ingest-cv`/`ingest-email` teamSize assertions exist — `fetch-job-content`'s `teamSize:3` claim is unverified by any test, only by direct source read above). |
| `cd web && npm test -- --run` | **PASS** — 52/52, 11 files | Web is out of scope for this change (untouched files); run only for completeness. |

## Spec Compliance Matrix (spec.md, cross-checked against `main`)

| Requirement | Evidence | Test | Result |
|---|---|---|---|
| `fetch-job-content` registered at `teamSize:3` | `worker/index.mjs:52-54` | `tests/index.test.mjs` boots handler, does not assert `teamSize:3` | ⚠️ PARTIAL — implemented, registration-boot tested, `teamSize` value untested |
| Handler re-validates `isHostAllowed` before Playwright | `fetch-job-content.mjs:44-47` | none found | ❌ UNTESTED |
| Generic `innerText` extraction, no per-host logic | `fetch-page.mjs:13-27` | none found | ❌ UNTESTED |
| Single attempt, no retry, NULL-only failure signal | `fetch-job-content.mjs:29-61` (4 early-return paths) | none found | ❌ UNTESTED |
| Tenant-scoped read/write via `tenantQuery` | `fetch-job-content.mjs:23-27,64-68` | none found (only `tests/lib/db.test.mjs` covers `tenantQuery` itself generically, not this caller) | ❌ UNTESTED |
| No new WS/endpoint, web polls existing route | Confirmed by absence — no new WS/route code added | N/A (negative requirement) | ✅ COMPLIANT (static) |
| `AddManual` gates enqueue on `lookupAllowedHost` | `service.go:146-160` | `TestLookupAllowedHost` covers the pure predicate only; **no test drives `AddManual` itself** to confirm the gate/upsert/enqueue wiring | ⚠️ PARTIAL — predicate tested, integration untested |
| Enqueue failure after successful upsert still returns job | `service.go:160-162` — `return &job, fmt.Errorf(...)` on enqueue error, upsert not rolled back. Confirmed by full read. | none found | ❌ UNTESTED |
| `ingest-email` enqueues unconditionally on `is_new` | `ingest-email.mjs:113,117-134` | none found | ❌ UNTESTED |
| `ingest-email` enqueue throw is caught, doesn't abort run | `ingest-email.mjs:129-134` (`try/catch`, non-fatal comment) | none found | ❌ UNTESTED |
| Duplicate job (`is_new` false) skips enqueue | `ingest-email.mjs:135-137` (`else { dupCount++ }`, no `boss.send` call in that branch) | none found | ❌ UNTESTED |

**Compliance summary**: 11 requirements checked, all **correctly implemented** by direct source inspection, but only 2/11 have any covering test (both partial), 9/11 are fully UNTESTED. Per Strict TDD's decision gate ("spec scenario has no passing covering test → CRITICAL"), this is a genuine, non-cosmetic gap — not a formality.

## TDD Compliance

| Check | Result | Details |
|---|---|---|
| TDD Evidence reported (apply-progress) | ❌ | No `apply-progress` artifact exists — no `sdd-apply` phase ever ran; code was written and merged before this project adopted the SDD pipeline for this change. |
| All tasks have tests | ❌ | 0/9 shipped tasks (T-1..T-9) have a dedicated test file; only pure helpers (`detectPlatform`, `lookupAllowedHost`) are tested, and those predate/are incidental to this change. |
| RED confirmed (tests exist) | ❌ | No test files exist for `fetch-job-content.mjs`, `fetch-page.mjs`, `isHostAllowed` (direct), the `ingest-email` enqueue call, or `AddManual`'s enqueue-gate behavior. |
| GREEN confirmed (tests pass) | ➖ N/A | Nothing to run — no tests exist for the new code paths. |
| Triangulation adequate | ➖ N/A | No test cases to triangulate. |
| Safety Net for modified files | ⚠️ | `service.go` and `ingest-email.mjs` were modified with zero regression coverage added for the new branches; pre-existing tests in both files still pass (confirmed by the live run above), so no *regression* was introduced, but the new branches themselves have no net. |

**TDD Compliance**: 0/9 tasks have TDD evidence — this is a known, explicitly-recorded exception, not an oversight. `design.md` Follow-up F-3 and `tasks.md`'s FU-1..FU-4 already document this exact gap and explicitly defer it ("no tests are to be written in this backfill"). Verify is surfacing it as CRITICAL per Strict TDD protocol regardless, since the protocol does not have a "pre-approved exception" bypass — but this is a repeat of an already-known, already-tracked decision, not a new discovery.

### Assertion Quality
No test files exist for the change's own code, so there is nothing to audit for trivial assertions. ✅ N/A — no new tests, therefore no trivial-test risk introduced.

### Test Layer Distribution
| Layer | Tests | Files | Tools |
|---|---|---|---|
| Unit | 0 | 0 | vitest (worker), `go test` (api) — both installed and used elsewhere, just not for this change |
| Integration | 0 | 0 | none |
| E2E | 0 | 0 | Playwright is present as a runtime dep (for the fetch itself), not used for testing this change |
| **Total** | **0** | **0** | |

## Correctness (Static Evidence)

| Requirement | Status | Notes |
|---|---|---|
| Worker consumer wiring (T-1..T-5) | ✅ Implemented | Verified byte-for-byte against spec.md's described behavior. |
| Go enqueue gate (T-6) | ✅ Implemented | Upsert-then-gate order matches spec exactly (non-allowlisted hosts still stored). |
| Dead sqlc query (T-7) | ✅ Implemented as documented dead code | Confirmed zero callers — matches the spec's own "Deviations" section, not a surprise. |
| Ingest-email wiring (T-8) | ✅ Implemented | Unconditional enqueue + non-fatal catch confirmed. |

## Coherence (Design)

| Decision | Followed? | Notes |
|---|---|---|
| D1 — async consumer, `teamSize:3` mirroring `generate-pdf` | ✅ Yes | `worker/index.mjs:53`. |
| D2 — generic `innerText`, single attempt, NULL-only signal | ✅ Yes | No per-host branching in `fetch-page.mjs`; no retry logic in `fetch-job-content.mjs`. |
| D3 — two independent SSRF gates (deviation, accepted) | ✅ Yes, as documented | Confirmed both `HOST_RULES` (JS) and `allowedHostPatterns` (Go) exist and are hand-synced; same 4 hosts, same ccSLD pattern shape. |
| D4 — asymmetric enqueue-time gating (deviation, accepted) | ✅ Yes, as documented | `AddManual` gates before enqueue; `ingest-email` enqueues unconditionally and relies on the worker's internal re-check. |
| D5 — dead `UpdateJobScrapedContent` sqlc query (deviation, accepted) | ✅ Yes, as documented | Confirmed unwired. |
| D6 — enqueue fails loudly on missing queue registration | Not independently re-verified this pass | Design.md attributes this to `queue.Enqueue`'s existing `RETURNING` contract in `boss.go`, which predates this change; not re-audited here since it's not new code for this change. |

## Findings

**CRITICAL**:
1. **Zero automated test coverage for every new/modified code path in this change** — `worker/jobs/fetch-job-content.mjs`, `worker/shared/fetch-page.mjs`, `isHostAllowed` (direct test), the `ingest-email.mjs` enqueue branch, and `AddManual`'s enqueue-gate branch all ship with no covering test, under a project where Strict TDD is the stated mode. This is not a new discovery — `design.md` (F-3) and `tasks.md` (FU-1..FU-4) already flag it explicitly and defer it on purpose — but per Strict TDD's verify protocol, an untested spec scenario is CRITICAL regardless of whether the gap was pre-announced. Recommend routing FU-1..FU-4 through `sdd-apply` before this change is considered fully closed, even though the code is already live and functionally correct.

**WARNING**:
1. `tests/index.test.mjs` boots `fetch-job-content`'s registration (proving the handler doesn't crash worker startup) but does not assert its `teamSize:3` value the way it does for `ingest-cv`/`ingest-email` — a silent regression (e.g. someone drops `teamSize:3`) would not be caught by the existing test even after FU-1 is done, unless that assertion is added too.
2. The two SSRF allowlists (`HOST_RULES` in JS, `allowedHostPatterns` in Go) are hand-synced with no shared source of truth (Decision 3, Follow-up F-5/FU-5) — confirmed still true, still a real drift risk, not resolved by this pass.
3. `UpdateJobScrapedContent` sqlc query remains dead code (FU-6) — confirmed unused; low risk but adds silent maintenance surface (must stay in sync with `jobs` schema for no functional benefit).

**SUGGESTION**:
1. When FU-1 is eventually implemented, prioritize the `handleFetchJobContent` happy path plus the 4 early-return branches (not found / bad URL / disallowed host / Playwright throw / empty text) — spec.md's scenarios already enumerate exactly these cases, so the test list requires no new design work.
2. Consider adding a `teamSize:3` assertion to `tests/index.test.mjs`'s `fetch-job-content` case for parity with the existing `ingest-cv`/`ingest-email` assertions — cheap, closes the WARNING above.

## Conclusion

The implementation on `main` matches `spec.md` and `design.md` on every
requirement checked by direct source inspection — all deviations recorded in
the retroactive docs were independently re-confirmed as accurate, and no
tasks.md checkbox corrections were needed (the backfill was accurate). All
three live test suites (`go test`, worker `vitest`, web `vitest`) pass today.

The one real, material gap is test coverage: under this project's Strict TDD
mode, a change that is functionally correct but has zero tests for its own
new code is not a clean PASS. **Verdict: PASS-WITH-GAPS** — safe to keep on
`main` as-is (it works, and the gap was consciously deferred, not hidden), but
FU-1..FU-4 (adding tests for `handleFetchJobContent`, `isHostAllowed`,
`ingest-email`'s enqueue call, and `AddManual`'s enqueue-gate behavior) should
be scheduled before this change is archived as fully done, not left indefinitely
as backlog.
