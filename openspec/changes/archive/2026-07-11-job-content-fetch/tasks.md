# Tasks: Job Content Fetch

> Retroactive backfill. The code for this change is already merged to `main`
> (PR #51, commit `8cc9f7d`, 2026-07-08). Tasks below document what shipped —
> checked items are history, not a forward plan. The only forward-looking
> section is "Follow-ups" at the end, which is a backlog record only.

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~180-220 (already merged, single PR) |
| 400-line budget risk | Low |
| Chained PRs recommended | No |
| Suggested split | N/A — shipped as one PR (#51) |
| Delivery strategy | N/A — retroactive, no apply phase |
| Chain strategy | N/A |

Decision needed before apply: No — nothing to apply, this is a backfill record.
Chained PRs recommended: No
400-line budget risk: Low

### Suggested Work Units (as shipped)

| Unit | Goal | PR | Notes |
|------|------|-----|-------|
| 1 | Worker consumer (`fetch-job-content` handler + `fetch-page.mjs` + registration) | #51 | New queue at `teamSize:3` |
| 2 | Go enqueue gate (`AddManual` + `allowedHostPatterns`) | #51 | Same PR, no split |
| 3 | Ingest-email enqueue wiring | #51 | Same PR, no split |

## Phase 1: Worker Consumer (Unit 1)

- [x] T-1 `worker/shared/fetch-page.mjs` — new `fetchPageText(url)`: launches headless Chromium, navigates with `waitUntil:'networkidle'` + 30s timeout, extracts `document.body.innerText`, closes browser. Shipped in commit `8cc9f7d`.
- [x] T-2 `worker/jobs/fetch-job-content.mjs` — new `handleFetchJobContent`: tenant-scoped read (`tenantQuery` SELECT `id, url`) → `isHostAllowed(hostname)` re-check → `fetchPageText` → tenant-scoped write (`tenantQuery` UPDATE `scraped_content`); every failure mode (not found, unparseable URL, disallowed host, Playwright throw, empty text) logs and returns without re-throw or retry. Shipped in commit `8cc9f7d`.
- [x] T-3 `worker/index.mjs` — register `fetch-job-content` at `teamSize: 3`. Shipped in commit `8cc9f7d`.
- [x] T-4 `worker/scripts/install-pgboss.mjs` — add `fetch-job-content` as the 6th entry in `QUEUE_NAMES`. Shipped in commit `8cc9f7d`.
- [x] T-5 `worker/lib/url-normalize.mjs` — export `isHostAllowed`, reusing existing `HOST_RULES` (linkedin/indeed/computrabajo/bumeran incl. ccSLDs). Shipped in commit `8cc9f7d`.

**Acceptance (T-1..T-5, as verified at merge time)**: worker boots with `fetch-job-content` registered; a `fetch-job-content` job with an allowlisted host navigates and writes `scraped_content`; a disallowed host, Playwright error, or empty extraction leaves `scraped_content` NULL with no retry.

## Phase 2: Go Enqueue Gate (Unit 2)

- [x] T-6 `api/internal/jobs/service.go` — add `allowedHostPatterns` (Go-side mirror of `HOST_RULES`) + `lookupAllowedHost`; `AddManual` checks the host BEFORE enqueueing `fetch-job-content`, but the job upsert proceeds unconditionally regardless of host. Shipped in commit `8cc9f7d`.
- [x] T-7 `db/queries/jobs.sql` + `api/internal/db/jobs.sql.go` — `UpdateJobScrapedContent` sqlc query added and regenerated (intended as the write path per original proposal). Shipped in commit `8cc9f7d` — note: unwired, see Follow-up F-2.

**Acceptance (T-6..T-7, as verified at merge time)**: `AddManual` with an allowlisted URL enqueues `fetch-job-content` with `{user_id, job_id}`; a non-allowlisted URL still stores the job row but does not enqueue; an enqueue error after a successful upsert returns the job alongside the error (no rollback).

## Phase 3: Ingest-Email Wiring (Unit 3)

- [x] T-8 `worker/jobs/ingest-email.mjs` — call `boss.send('fetch-job-content', {user_id, job_id})` unconditionally for every newly-inserted job row (`is_new` via `xmax = 0`), regardless of host; enqueue failures are caught, logged, and do not abort the ingest run. Shipped in commit `8cc9f7d`.

**Acceptance (T-8, as verified at merge time)**: a new job from email ingestion enqueues `fetch-job-content`; a duplicate (`ON CONFLICT DO NOTHING`, zero rows) does not enqueue; an enqueue throw is logged and the run continues to the next message.

## Phase 4: Cross-cutting Verification (as run at merge time)

- [x] T-9 `make test-all` run at merge time — Go/worker/web suites passed. Note: this covered only pre-existing test surfaces; no new tests were added for this change's own code paths (see Follow-up F-3). Shipped in commit `8cc9f7d`.

## Dependencies Between Slices (as shipped)

- Unit 1 (worker consumer) has no compile-time dependency on Unit 2 or 3 — it's the consumption side, reachable independent of who enqueues.
- Unit 2 (`AddManual`) and Unit 3 (`ingest-email`) are independent enqueue call sites; both target the same queue name from Unit 1's registration.
- All three units landed in the single PR #51 / commit `8cc9f7d` — no chaining was used.

---

## Follow-ups (not implemented, tracked only)

The items below are recorded as an outstanding backlog per `design.md`'s Open
Questions / Follow-ups (F-1, F-2, F-3, O-1). They are explicitly **NOT** to be
implemented as part of this backfill — no code, no tests, no schema changes
are to be made against these items right now. Listed here only so they are
not lost.

- [x] FU-1 (F-3) Add a unit test for `worker/jobs/fetch-job-content.mjs`'s `handleFetchJobContent`, covering: job not found, disallowed host, Playwright throw, empty/whitespace extraction, and the happy path write via `tenantQuery`. Test: `worker/tests/jobs/fetch-job-content.test.mjs` (6 tests).
- [x] FU-2 (F-3) Add a unit test for `isHostAllowed` (`worker/lib/url-normalize.mjs`), covering allowlisted hosts (linkedin/indeed/computrabajo/bumeran incl. ccSLD variants) and rejection of arbitrary hosts. Test: added `describe('isHostAllowed', ...)` block to `worker/tests/lib/url-normalize.test.mjs`.
- [x] FU-3 (F-3) Add a test for `ingest-email.mjs`'s `fetch-job-content` enqueue call, covering: new job enqueues, duplicate job does not enqueue, and enqueue throw is caught/logged without aborting the ingest run. Test: added `describe('fetch-job-content enqueue', ...)` block to `worker/tests/jobs/ingest-email.test.mjs`.
- [x] FU-4 (F-3) Add a test for `AddManual`'s enqueue-on-allowlisted-host behavior (`api/internal/jobs/service.go`), covering: allowlisted host enqueues, non-allowlisted host stores the job but skips enqueue, and enqueue error returns the job alongside the error. Test: `api/internal/jobs/addmanual_enqueue_integration_test.go` (`TestAddManual_EnqueueGate`, DB-gated via `TEST_DATABASE_URL`, skips cleanly otherwise).
- [ ] FU-5 (F-1) De-duplicate the SSRF allowlist currently implemented independently in `worker/lib/url-normalize.mjs` (JS) and `api/internal/jobs/service.go` (Go) — establish a shared source of truth to remove hand-sync drift risk.
- [ ] FU-6 (F-2) Remove (or finally wire) the dead `UpdateJobScrapedContent` sqlc query in `db/queries/jobs.sql` / `api/internal/db/jobs.sql.go` — currently unused in the API.
- [ ] FU-7 (O-1) Add monitoring/alerting for combined Chromium memory pressure from `generate-pdf` and `fetch-job-content` sharing `shm_size:1gb` at `teamSize:3` each; define the upgrade path (lower team size or raise `shm_size`) if OOM is observed.
