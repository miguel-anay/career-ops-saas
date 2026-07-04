# Proposal: Job Content Fetch

Manual and email-ingested jobs (Bumeran, LinkedIn, Computrabajo, Indeed) never get `jobs.scraped_content` populated — only the 6 ATS providers do. The sibling change `evaluation-quality` ships an `ErrJobContentMissing` 422 guard that blocks evaluation on those jobs forever until their JD is fetched. This change fetches the JD asynchronously via a new pg-boss job so those jobs become evaluable end-to-end, clearing the 422. Empirical stakes: the user's Bumeran job scored 3.4/5 with CV but no JD; their prior agent workflow with full JD scored 3.9/5.

## Intent

- **Problem**: `evaluation-quality`'s 422 guard is a permanent dead end for non-ATS jobs because nothing ever populates their `scraped_content`.
- **Why now**: the guard ships in the sibling change; without this, every manual/email job returns "JD unavailable" with no path to recovery.
- **Success**: a job added by URL (or ingested from email) gets its JD text populated asynchronously, the sibling 422 clears, and the Bumeran job evaluates end-to-end.

## Scope

### In Scope
- New pg-boss job type `fetch-job-content` (6th queue, `teamSize:3`) in `worker/`.
- Playwright-based fetch, uniform across all hosts (`page.innerText` generic text extraction — no per-host selectors).
- SSRF allowlist gate via `url-normalize.mjs`'s `HOST_RULES`, enforced BEFORE Playwright navigates.
- Enqueue from `AddManual` (Go API) and from `ingest-email` (worker) when `scraped_content IS NULL`.
- New sqlc query `UpdateJobScrapedContent`; worker write via `tenantQuery` (RLS-enforced).
- Web reads result by polling existing `GET /api/jobs/{id}` — no WS, no new endpoint.

### Out of Scope
- **Web-search / salary enrichment** (`SERPER_API_KEY` is dead config today) — a third, separate, opt-in future change with a different trust boundary and per-eval cost.
- **Per-host detail parsers** — no precedent, high maintenance, breaks on redesigns.
- **Retry / observability columns** (`content_fetch_status`, `content_fetch_error`) — follow-on only if failure rate proves non-trivial. MVP = single attempt, `scraped_content` NULL stays the sole signal.
- **Any LLM usage** — this is a 0-token change.
- **New WS channel / per-job progress** — disproportionate for one fire-and-forget job.

## Capabilities

### New Capabilities
- `worker-fetch-job-content`: a `fetch-job-content` pg-boss consumer renders a job URL with Playwright, extracts text, and writes `scraped_content` under the job's tenant.

### Modified Capabilities
- `jobs-manual-create` (`api/internal/jobs`): `AddManual` applies the `HOST_RULES` SSRF gate and enqueues `fetch-job-content` when `scraped_content` is absent.
- `worker-ingest-email`: after upserting an email-ingested job with NULL `scraped_content`, enqueues `fetch-job-content`.

## Approach

Follows the exploration's recommendation. One new async job, no new deps, no new DB columns, no new infra beyond a queue registration.

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Fetch mechanism | Playwright (`page.innerText`) | Already a dep (`pdf.mjs`); works regardless of SSR vs SPA, so the SPA spike is sidestepped, not resolved |
| Extraction | Generic text, not per-host | No readability/cheerio installed, no per-host precedent; fidelity tradeoff acceptable for MVP |
| Trust boundary | `HOST_RULES` gate at `AddManual` BEFORE navigation | `AddManual` accepts arbitrary `https://` today — this is the real new SSRF surface |
| Concurrency | `teamSize:3`, mirroring `generate-pdf` | Chromium is memory-heavy; matches existing Playwright consumer |
| Result delivery | Web polls `GET /api/jobs/{id}` | Fire-and-forget; building a run/WS concept for one job is over-engineering |
| Retry/status | Single attempt, NULL = only signal | Matches how the sibling 422 guard already reads `scraped_content` |

Control/data plane split is preserved: the Go API only enqueues (`api/internal/queue/boss.go`), never scrapes. The worker performs the fetch and writes via `tenantQuery` so RLS is enforced.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `worker/jobs/fetch-job-content.mjs` | New | Playwright render + `page.innerText` + tenant write |
| `worker/index.mjs` | Modified | Register `fetch-job-content` handler at `teamSize:3` |
| `worker/scripts/install-pgboss.mjs` (`QUEUE_NAMES`) | Modified | Add 6th queue name — hardened `boss.go` enqueue fails loudly if missing |
| `worker/jobs/ingest-email.mjs` | Modified | 2nd enqueue point when `scraped_content IS NULL` |
| `worker/lib/url-normalize.mjs` (`HOST_RULES`) | Reused | SSRF allowlist source, no change expected |
| `api/internal/jobs/service.go` (`AddManual`) | Modified | SSRF gate + enqueue |
| `api/internal/queue/boss.go` callers | Modified | New enqueue call for `fetch-job-content` |
| `db/queries/jobs.sql` | New query | `UpdateJobScrapedContent` (regen sqlc) |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| **SSRF** — `AddManual` accepts arbitrary `https://` today; a malicious URL could target internal services | **High if ungated** | Enforce `HOST_RULES` allowlist at `AddManual` BEFORE Playwright navigates. Reject non-allowlisted hosts at API boundary, never store or enqueue them. This is the single most important control in the change |
| SPA-vs-SSR behavior unverified for all 4 hosts | Med | Playwright renders JS, so this is de-risked, not resolved; if a host still yields empty text, `scraped_content` stays NULL and the 422 persists (safe failure) |
| Two concurrent Chromium job types share `shm_size:1gb` (`docker-compose.yml:65`) | Med | Start at `teamSize:3`; if OOM appears, lower team size or raise `shm_size`. Untested — monitor |
| Forgetting the 6th `QUEUE_NAMES` entry | Low | Hardened `boss.go` enqueue fails loudly (not silently); caught at first enqueue |
| No retry/observability in MVP — a failed fetch has no self-service recovery | Med | Documented follow-on: add `content_fetch_status/error` columns if failure rate proves non-trivial |
| Scope creep from `SERPER_API_KEY` / enrichment | Low | Explicitly out of scope; separate future change |

## Rollback Plan

- Revert the `AddManual` enqueue + SSRF gate commit → manual jobs return to prior (NULL `scraped_content`, 422-blocked) behavior. No data loss.
- Revert the worker handler + `QUEUE_NAMES` entry → `fetch-job-content` jobs stop being consumed; enqueue would then fail loudly, so revert both sides together.
- Revert `ingest-email` enqueue point independently.
- No schema migration to undo (`UpdateJobScrapedContent` only writes an existing nullable column). Rolling back leaves already-populated `scraped_content` intact and evaluable — no cleanup needed.

## Dependencies

- **None new.** Playwright already in `worker/package.json`; `HOST_RULES` already covers bumeran/linkedin/indeed/computrabajo (`worker/lib/url-normalize.mjs:24-29`).
- **Depends on** `evaluation-quality` shipping the `ErrJobContentMissing` 422 guard — that is the gate this change clears. This change is only meaningful once that guard exists.

## Success Criteria

- [ ] A job added by URL on an allowlisted host gets `scraped_content` populated asynchronously within N minutes.
- [ ] The sibling `ErrJobContentMissing` 422 clears for that job and it evaluates end-to-end — specifically the Bumeran job becomes evaluable.
- [ ] A URL on a non-allowlisted host is rejected at `AddManual` before any navigation, is never stored, and is never enqueued (SSRF gate proven).
- [ ] Email-ingested jobs with NULL `scraped_content` trigger a `fetch-job-content` enqueue.
- [ ] Worker writes `scraped_content` under the correct tenant via `tenantQuery` (RLS holds — no cross-tenant write).
- [ ] Zero LLM tokens consumed by this change.
- [ ] A fetch that yields no usable text leaves `scraped_content` NULL and the 422 in place (safe failure, no garbage stored).
