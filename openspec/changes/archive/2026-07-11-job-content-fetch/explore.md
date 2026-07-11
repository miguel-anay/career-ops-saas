# Exploration: job-content-fetch

> Mirror of engram `sdd/job-content-fetch/explore` (obs #376), 2026-07-03.

## Problem

Manual and email-ingested jobs never get `jobs.scraped_content` populated — only the 6 ATS providers do, which blocks the sibling change `evaluation-quality`'s `ErrJobContentMissing` 422 guard from ever clearing for those jobs.

## Current State

- `worker/jobs/scan.mjs:11-18` — only the 6 ATS providers populate `scraped_content`.
- `api/internal/jobs/service.go:32-70` (`AddManual`) accepts any `https://` URL, always leaves `scraped_content` NULL; `detectPlatform` (`service.go:118-136`) resolves Bumeran/LinkedIn/Indeed/Computrabajo to `"unknown"`.
- `api/internal/jobs/handler.go:35-39,72-95` — only route creating manual jobs; no job-status/update route exists.
- `worker/jobs/ingest-email.mjs:100-129` and `worker/email-parsers/_shared.mjs:25` (`JOB_CARD_RE`) — email parsers only ever extract title/company/url from the email card, never the JD body; no existing per-host body-extraction code to reuse.
- `db/schema.sql:80-94` — `jobs.scraped_content text` nullable; no fetch-status/error columns; `job_status_t` (`schema.sql:12`) has no `failed` value.
- SSRF precedent to reuse: `worker/lib/gmail.mjs:8,13-24` and `worker/lib/url-normalize.mjs:24-29` (`HOST_RULES`, already covers bumeran/linkedin/indeed/computrabajo).
- Playwright precedent: `worker/jobs/pdf.mjs:147-208` → `worker/shared/generate-pdf.mjs:14-46` — launches/closes Chromium per job call, no pool.
- Concurrency precedent: `worker/index.mjs:25-49` — `generate-pdf` (Chromium) at `teamSize:3` vs `scan-company` (network only) at `teamSize:10`; `docker-compose.yml:65` `shm_size:"1gb"` sized against a single Playwright consumer.
- Queue registration: `worker/scripts/install-pgboss.mjs:39` `QUEUE_NAMES` (currently 5) must gain a 6th entry or `api/internal/queue/boss.go:129-160`'s hardened enqueue fails loudly.
- WS: `api/internal/ws/hub.go:33-38` is hard-keyed to `scan_run_id`; `worker/lib/progress.mjs:15-24` always assumes a run row — no generic per-job channel exists.
- `SERPER_API_KEY` (`docker-compose.yml:61`) has zero code references anywhere in `worker/` — dead config for an unimplemented feature.

## Affected Areas

- `api/internal/jobs/service.go`, `db/queries/jobs.sql` (new `UpdateJobScrapedContent`), `api/internal/queue/boss.go` callers
- `worker/scripts/install-pgboss.mjs:39`, `worker/index.mjs` (new handler registration)
- new `worker/jobs/fetch-job-content.mjs`, `worker/jobs/ingest-email.mjs:100-129` (second enqueue point)
- `worker/lib/url-normalize.mjs` (`HOST_RULES` reused for the SSRF gate)

## Approaches

| Approach | Pros | Cons | Effort |
|---|---|---|---|
| Playwright render, uniform across hosts | Zero new deps, works regardless of SSR/SPA, reuses `pdf.mjs` lifecycle | Heavier, adds 2nd Chromium-concurrent job type sharing `shm_size` | Medium |
| Plain HTTP fetch + generic extraction | Cheapest | Dead end on SPA hosts — unverified without a live network call (spike, not resolvable here) | Low or dead-end |
| Per-host detail parsers | Highest fidelity | High maintenance, no existing precedent, breaks on redesigns | High |

Extraction: no readability/cheerio/jsdom installed and no per-host body-extraction precedent exists — recommend generic text extraction (e.g. `page.innerText`) over per-host selectors for MVP.

Retry/failure: no fetch-status/error columns exist. Recommend MVP = single attempt, no new columns — `scraped_content` NULL stays the sole signal (matches how the sibling 422 guard already reads it); flag adding `content_fetch_status/error` columns as a follow-on if failure rate proves non-trivial.

WS/progress: disproportionate to build a run concept for one job — fire-and-forget, web polls existing `GET /api/jobs/{id}`.

Concurrency: mirror `pdf.mjs`'s `teamSize:3`, but flag combined Chromium memory pressure against the existing `shm_size:1gb` as untested.

SERPER_API_KEY / web-search enrichment: dead code, different trust boundary (external search API + per-eval cost) — keep as a third, separate, opt-in future change; do not fold in here. This matches evaluation-quality's own proposal.md conclusion.

## Recommendation

Single new pg-boss job `fetch-job-content` (6th queue, `teamSize:3`), Playwright uniformly (skip the SSR/SPA spike as a blocking gate — Playwright sidesteps needing the answer), generic text extraction, SSRF-gated via `url-normalize.mjs`'s `HOST_RULES` applied at `AddManual` time before Playwright ever navigates. No new DB columns, no WS.

## Risks

- SSRF: `AddManual` is the real new trust boundary (arbitrary `https://` today) — must gate before navigation, not just before storage.
- SPA-vs-SSR unverified for all 4 hosts (deliberately not spiked here — no network calls made).
- Two concurrent Chromium job types now share `shm_size:1gb` — untested.
- Forgetting to add the 6th queue to `install-pgboss.mjs:39` causes a loud (not silent) enqueue failure per the hardened `boss.go` check — but still an easy miss.
- No retry/observability in MVP — a failed manual job has no self-service recovery.
- Scope creep risk from `SERPER_API_KEY`/enrichment — explicitly out of scope.

## Ready for Proposal

Yes.
