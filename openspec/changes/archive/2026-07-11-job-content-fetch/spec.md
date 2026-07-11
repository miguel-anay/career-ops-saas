# Spec Delta: job-content-fetch

> Retroactive spec. The code for this change is already merged to `main`
> (PR #51, commit `8cc9f7d`, 2026-07-08). This document describes what was
> ACTUALLY SHIPPED, grounded in the current code, not a forward plan. See
> "Deviations from proposal" at the end for drift against `proposal.md`.

## Domain: worker-fetch-job-content (NEW)

### Requirement: New `fetch-job-content` pg-boss queue at teamSize 3

The worker MUST register a `fetch-job-content` job handler (`worker/jobs/fetch-job-content.mjs`, wired in `worker/index.mjs`) at `teamSize: 3`, and the queue name MUST be present in `QUEUE_NAMES` (`worker/scripts/install-pgboss.mjs`) so `api/internal/queue/boss.go`'s enqueue does not fail with "queue not registered".

#### Scenario: Worker boots with the new handler registered

- GIVEN the worker process starts
- WHEN `registerWorker('fetch-job-content', handleFetchJobContent, { teamSize: 3 })` runs
- THEN subsequent `fetch-job-content` jobs enqueued by either the Go API or `ingest-email.mjs` are picked up and processed

### Requirement: Handler re-validates the SSRF allowlist before navigating

`handleFetchJobContent` MUST re-check `isHostAllowed(hostname)` (`worker/lib/url-normalize.mjs`) against the job's stored `url` before invoking Playwright, independent of any gate already applied upstream (defense in depth — the worker does not trust that every enqueue path pre-filtered the host).

#### Scenario: Job URL host is not in the allowlist

- GIVEN a `fetch-job-content` job whose `jobs.url` hostname does not match `HOST_RULES` (linkedin/indeed/computrabajo/bumeran, including ccSLD variants)
- WHEN the handler runs
- THEN it logs the rejection and returns without calling Playwright and without writing `scraped_content`

#### Scenario: Job URL host is allowlisted

- GIVEN a `fetch-job-content` job whose `jobs.url` hostname matches `HOST_RULES`
- WHEN the handler runs
- THEN it proceeds to Playwright navigation

### Requirement: Generic Playwright text extraction, no per-host parsing

`worker/shared/fetch-page.mjs`'s `fetchPageText(url)` MUST launch headless Chromium, navigate with `waitUntil: 'networkidle'` and a 30-second timeout, extract `document.body.innerText`, and close the browser — uniformly across all allowlisted hosts. No per-host selector or extraction logic exists.

#### Scenario: Page renders successfully

- GIVEN an allowlisted job URL that resolves to a live page
- WHEN `fetchPageText` navigates and the page reaches network-idle
- THEN the function returns the page's visible `innerText` as a string

### Requirement: Single attempt, no retry, `scraped_content` NULL is the only failure signal

The handler MUST NOT retry or record a failure status. Any of: job row not found, unparseable `url`, disallowed host, Playwright throwing, or empty/whitespace-only extracted text, MUST result in the handler logging the error and returning normally (no re-throw) — leaving `jobs.scraped_content` NULL. No new columns (e.g. `content_fetch_status`, `content_fetch_error`) exist.

#### Scenario: Playwright navigation throws

- GIVEN an allowlisted job URL that times out or errors during `page.goto`
- WHEN the handler catches the exception
- THEN it logs the failure, does not write `scraped_content`, and does not re-enqueue or retry

#### Scenario: Extraction yields empty text

- GIVEN Playwright successfully navigates but `innerText` is empty or whitespace-only
- WHEN the handler checks the extracted text
- THEN it logs and returns without writing `scraped_content`

### Requirement: Tenant-scoped read and write via `tenantQuery`

The handler MUST read the job row (`SELECT id, url FROM jobs WHERE id = $1`) and write the result (`UPDATE jobs SET scraped_content = $1 WHERE id = $2`) exclusively through `tenantQuery(user_id, ...)` (`worker/lib/db.mjs`), so RLS's `SET LOCAL app.current_user_id` scopes both operations to the job's owning tenant. The payload MUST be `{ user_id, job_id }`.

#### Scenario: Job belongs to the enqueuing user

- GIVEN a `fetch-job-content` job with `{ user_id, job_id }` where `job_id` belongs to `user_id`
- WHEN the handler reads and later writes the row
- THEN both operations succeed under that tenant's RLS context

#### Scenario: Job row not found under the given tenant

- GIVEN a `job_id` that does not exist (or does not belong to `user_id`, so RLS hides it)
- WHEN the handler's initial SELECT returns zero rows
- THEN it logs the miss and returns without attempting Playwright or a write

### Requirement: No WebSocket delivery — web polls the existing job-read endpoint

This change introduces no new WS channel, run concept, or endpoint. Once `scraped_content` is written, the existing `GET /api/jobs/{id}` (already used by the web client) reflects the populated value on its next poll/read.

#### Scenario: Web re-reads a job after content fetch completes

- GIVEN a job whose `fetch-job-content` job has finished successfully
- WHEN the web client calls `GET /api/jobs/{id}` (existing route, unmodified)
- THEN the response includes the newly populated `scraped_content`, and the sibling `evaluation-quality` 422 (`job_content_missing`) no longer applies to that job

## Domain: jobs-manual-create (MODIFIED)

### Requirement: `AddManual` applies an SSRF allowlist gate before enqueueing

`Service.AddManual` (`api/internal/jobs/service.go`) MUST check the parsed URL's hostname against `allowedHostPatterns` (a Go-side mirror of the worker's `HOST_RULES`: linkedin.com, indeed.com, computrabajo.com incl. ccSLDs, bumeran.com incl. ccSLDs) via `lookupAllowedHost`. The job upsert MUST proceed unconditionally (existing behavior preserved — arbitrary `https://` URLs are still stored); only the `fetch-job-content` enqueue is gated on the host check.

#### Scenario: Manual job on an allowlisted host

- GIVEN a user calls `AddManual` with `https://www.bumeran.com.pe/empleos/...`
- WHEN the upsert succeeds
- THEN `fetch-job-content` is enqueued with `{ user_id, job_id }`

#### Scenario: Manual job on a non-allowlisted host

- GIVEN a user calls `AddManual` with a URL whose host is not in `allowedHostPatterns` (e.g. an arbitrary company career page)
- WHEN the upsert succeeds
- THEN the job row is stored as before, but `fetch-job-content` is NOT enqueued, and no Playwright navigation to that host is ever attempted from this path
- AND `scraped_content` stays NULL (the job remains 422-blocked by the sibling `evaluation-quality` guard until a JD is otherwise supplied)

#### Scenario: Enqueue failure after a successful upsert

- GIVEN the host is allowlisted and `queue.Enqueue` returns an error (e.g. the queue is not registered)
- WHEN `AddManual` handles that error
- THEN it still returns the already-created `job` alongside the enqueue error (the row is not rolled back)

## Domain: worker-ingest-email (MODIFIED)

### Requirement: Email-ingested jobs enqueue `fetch-job-content` unconditionally on creation

`handleIngestEmail` (`worker/jobs/ingest-email.mjs`) MUST call `boss.send('fetch-job-content', { user_id, job_id })` for every newly-inserted job row (`is_new` from the upsert's `xmax = 0` check), regardless of host — the enqueue itself is not host-gated at this call site; the worker handler's own `isHostAllowed` check (see worker-fetch-job-content domain) is the sole enforcement point for this ingestion path. Enqueue failures MUST be caught and logged, and MUST NOT fail the enclosing email-ingest run.

#### Scenario: New job from a recognized email sender

- GIVEN `handleIngestEmail` upserts a job row and the insert reports `is_new: true`
- WHEN the row is committed
- THEN `fetch-job-content` is enqueued for `{ user_id, job_id }`, independent of which platform parser produced it

#### Scenario: Duplicate job (already existed)

- GIVEN the upsert's `ON CONFLICT (user_id, url) DO NOTHING` yields zero returned rows (duplicate)
- WHEN `handleIngestEmail` processes that result
- THEN no `fetch-job-content` enqueue happens for that row

#### Scenario: Enqueue call itself fails

- GIVEN `boss.send('fetch-job-content', ...)` throws
- WHEN `handleIngestEmail` catches the error
- THEN it logs the failure and continues processing remaining messages — the ingest run does not abort (NFR-07 pattern)

## Deviations from proposal

- **`UpdateJobScrapedContent` sqlc query is unused.** The proposal lists a new sqlc query `UpdateJobScrapedContent` (`db/queries/jobs.sql`) as the write path ("worker write via `tenantQuery`"). The query was added and regenerated into `api/internal/db/jobs.sql.go`, but no Go handler/service calls it — the actual write is a raw inline SQL string (`UPDATE jobs SET scraped_content = $1 WHERE id = $2::uuid`) executed by the worker through `tenantQuery`, since the worker is Node and cannot invoke Go's sqlc-generated `Queries` methods. The sqlc query is currently dead code in the API; it is not wired to any route.
- **Two independent SSRF gates, not one shared check.** The proposal describes "the `HOST_RULES` gate... enforced BEFORE Playwright navigates" as a single control. Shipped code has two separately-maintained implementations of the same allowlist: `HOST_RULES`/`isHostAllowed` in `worker/lib/url-normalize.mjs` (JS regexes) and `allowedHostPatterns`/`lookupAllowedHost` in `api/internal/jobs/service.go` (Go regexes, commented as mirroring the worker list). They must be kept in sync by hand; there is no shared source of truth across languages.
- **Asymmetric enqueue-time gating between the two call sites.** `AddManual` (Go) checks the host allowlist BEFORE enqueueing (non-allowlisted hosts are never enqueued). `ingest-email.mjs` (worker) enqueues unconditionally for every new job and relies entirely on the worker handler's internal `isHostAllowed` check to reject disallowed hosts at consumption time. End state is equivalent (no Playwright navigation to disallowed hosts either way), but the two ingestion paths are inconsistent in where the gate lives. Not called out as a discrepancy in the proposal, which describes both enqueue points uniformly.
- **No automated test coverage for the feature's integration paths.** `worker/jobs/fetch-job-content.mjs`, `worker/shared/fetch-page.mjs`, and the `isHostAllowed` function have zero test files. The `ingest-email.mjs` enqueue call and `AddManual`'s enqueue-on-allowlisted-host behavior are also untested — only the pure helper functions `detectPlatform` and `lookupAllowedHost` (Go) have unit tests (`api/internal/jobs/service_test.go`). The proposal's Success Criteria checklist implies these behaviors should be provable (e.g. "SSRF gate proven"), but no test in the repo currently proves them.
- **Combined Chromium memory pressure risk (flagged, not resolved).** The proposal calls out `generate-pdf` and `fetch-job-content` sharing `shm_size:1gb` as "untested." This spec confirms no code change addresses it — it remains an open operational risk, not a shipped mitigation.
