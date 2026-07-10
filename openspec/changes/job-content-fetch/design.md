# Design: Job Content Fetch

> Retroactive design. The code is already merged to `main` (PR #51, commit
> `8cc9f7d`, 2026-07-08). This documents the architecture ACTUALLY SHIPPED,
> grounded in the current code — not a forward plan. Ground truth for drift is
> `spec.md`'s "Deviations from proposal" section; every deviation is recorded
> below as a DECIDED fact, not an open question.

## Technical Approach

One new async pg-boss consumer (`fetch-job-content`, the 6th queue) renders a
job URL with Playwright, extracts generic `innerText`, and writes
`jobs.scraped_content` under the job's tenant via `tenantQuery`. Two enqueue
call sites feed it: `AddManual` (Go API) and `ingest-email` (worker). The
change adds zero new deps, zero new schema (writes an existing nullable
column), zero LLM tokens, and no new WS/endpoint — web reads the result by
polling the existing `GET /api/jobs/{id}`. The control/data-plane split holds:
Go only enqueues (`queue.Enqueue`), the worker does all navigation and writes.

The SSRF allowlist is the single most important control. It is enforced at TWO
independent layers (Go enqueue-gate + worker consume-gate), by design (see
Decision 3).

## Architecture Decisions

### Decision 1 — Async pg-boss consumer, mirroring `generate-pdf`

**Choice**: A new `fetch-job-content` handler
(`worker/jobs/fetch-job-content.mjs`) registered in `worker/index.mjs` at
`teamSize: 3`, with the queue name added to `QUEUE_NAMES`
(`worker/scripts/install-pgboss.mjs`). Chromium launch lives in a shared
`worker/shared/fetch-page.mjs` (`fetchPageText`), launched per-call exactly
like `pdf.mjs`.

| Option | Tradeoff | Decision |
|--------|----------|----------|
| New async queue at `teamSize:3` | Matches existing Chromium consumer; back-pressure via team cap | **Chosen** |
| Synchronous fetch inside `AddManual` | Blocks the API request on a 30s Playwright nav; couples control plane to Chromium | Rejected — violates plane split |
| Reuse `scan-company` / `evaluate-job` | Different lifecycle, different concurrency profile | Rejected |

`teamSize:3` mirrors `generate-pdf` because both are memory-heavy Chromium
consumers sharing `shm_size:1gb` (see Open Question O-1).

### Decision 2 — Generic `innerText` extraction, single attempt, NULL is the only signal

**Choice**: `fetchPageText` navigates with `waitUntil:'networkidle'` + 30s
timeout, returns `document.body.innerText`, uniform across all hosts — no
per-host selectors. Every failure mode (row not found, unparseable URL,
disallowed host, Playwright throw, empty/whitespace text) is caught, logged,
and returns normally. Nothing is re-thrown, nothing is retried, and
`scraped_content` stays NULL. No `content_fetch_status`/`content_fetch_error`
columns exist.

| Option | Tradeoff | Decision |
|--------|----------|----------|
| Generic `innerText` | Zero maintenance; works SSR or SPA (JS renders) | **Chosen** |
| Per-host readability/cheerio parsers | Higher fidelity; breaks on redesigns, new dep, no precedent | Rejected — YAGNI |
| Retry + status columns | Self-service recovery | Rejected — schema churn; NULL already reads as "needs fetch" |

Rationale for NULL-as-signal: the sibling `evaluation-quality` 422 guard
already reads `scraped_content` emptiness. A failed fetch leaving NULL keeps
the job safely 422-blocked with no garbage stored — a safe failure, not a
broken one.

### Decision 3 — Two independent SSRF gates, not one shared source of truth (DEVIATION)

**Choice**: The allowlist is implemented TWICE — `HOST_RULES` / `isHostAllowed`
in `worker/lib/url-normalize.mjs` (JS regexes) and `allowedHostPatterns` /
`lookupAllowedHost` in `api/internal/jobs/service.go` (Go regexes, commented as
mirroring the worker list). They are hand-synced; there is no cross-language
shared config.

**Rationale**: Go and Node do not share a runtime. A single source of truth
would require either a shared config file both languages parse at boot or an
RPC round-trip on every enqueue. The shipped code accepts hand-sync drift risk
to avoid that coupling. This is a deliberate tradeoff, recorded as-is — see
Follow-up F-1; not resolved here.

### Decision 4 — Defense-in-depth: worker re-validates the host before navigating (DEVIATION, asymmetric gating)

**Choice**: `handleFetchJobContent` re-runs `isHostAllowed(hostname)` on the
job's stored URL before calling Playwright, regardless of any upstream gate.
The two enqueue sites gate differently:

- `AddManual` (Go) checks `lookupAllowedHost` BEFORE enqueueing —
  non-allowlisted hosts are never enqueued (but the job row is still stored,
  preserving existing behavior).
- `ingest-email` (worker) enqueues UNCONDITIONALLY for every newly-inserted
  job (`is_new` via `xmax = 0`), relying entirely on the worker handler's own
  `isHostAllowed` gate at consume time.

**Rationale**: The worker gate is the true trust boundary because it is the
last check before navigation and it does not trust that every enqueue path
pre-filtered. Given that gate exists, `ingest-email` needs no duplicate
enqueue-side check, while `AddManual` keeps its check to avoid enqueuing
provably-dead work. End state is equivalent — Playwright never navigates to a
disallowed host on either path — but the gate lives in different places per
path. Recorded as a decided fact, not a discrepancy to fix.

### Decision 5 — Worker writes via raw inline SQL through `tenantQuery`; the sqlc `UpdateJobScrapedContent` query is dead code (DEVIATION)

**Choice**: The handler writes with a raw string —
`UPDATE jobs SET scraped_content = $1 WHERE id = $2::uuid` — executed through
`tenantQuery(user_id, ...)` (`worker/lib/db.mjs`), which wraps it in
`SET LOCAL app.current_user_id` so RLS scopes the write to the owning tenant.
The read is likewise `SELECT id, url FROM jobs WHERE id = $1::uuid` via
`tenantQuery`. The payload is `{ user_id, job_id }`.

**Rationale**: The write path is the Node worker, which cannot invoke Go's
sqlc-generated `Queries` methods. The `UpdateJobScrapedContent` sqlc query was
added to `db/queries/jobs.sql` and regenerated into
`api/internal/db/jobs.sql.go`, but no Go route or service calls it — it is dead
code in the API. Recorded as-is; removal deferred to Follow-up F-2.

### Decision 6 — Hardened enqueue fails loudly on missing queue registration

**Choice**: `queue.Enqueue` (`api/internal/queue/boss.go`) hand-replicates
pg-boss v10.4.2's `insertJob` SQL. Its `JOIN pgboss.queue` yields zero rows if
`fetch-job-content` was never registered via `createQueue`; the shipped code
treats a zero-row `RETURNING` as an explicit error rather than pg-boss's silent
null. `AddManual` propagates that error but returns the already-created `job`
alongside it (the upsert is not rolled back).

**Rationale**: Forgetting the 6th `QUEUE_NAMES` entry becomes a loud
first-enqueue failure, not silent job loss. This is why `pg-boss` is pinned to
an EXACT version — the SQL contract must not drift under a patch bump.

## Data Flow

    AddManual(url) ──upsert(job)──▶ host allowlisted?
       │                              │ yes → queue.Enqueue(fetch-job-content,{user_id,job_id})
       │                              │ no  → stored, NOT enqueued (stays 422-blocked)
    ingest-email ──upsert(is_new)──▶ boss.send(fetch-job-content,{user_id,job_id})  [unconditional]
                                       │
                                       ▼
    worker handleFetchJobContent ─ tenantQuery SELECT url
                                 ─ isHostAllowed? ─ no → log+return (NULL stays)
                                 ─ fetchPageText (Playwright, networkidle, 30s)
                                 ─ empty/throw? ─ log+return (NULL stays)
                                 ─ tenantQuery UPDATE scraped_content
                                       │
    web ── GET /api/jobs/{id} (poll, existing route) ──▶ scraped_content populated → 422 clears

## File Changes (as shipped)

| File | Action | Description |
|------|--------|-------------|
| `worker/jobs/fetch-job-content.mjs` | New | Handler: tenant read → SSRF gate → fetch → tenant write |
| `worker/shared/fetch-page.mjs` | New | `fetchPageText` — headless Chromium `innerText` extractor |
| `worker/index.mjs` | Modify | Register `fetch-job-content` at `teamSize:3` |
| `worker/scripts/install-pgboss.mjs` | Modify | 6th `QUEUE_NAMES` entry |
| `worker/jobs/ingest-email.mjs` | Modify | Unconditional `boss.send('fetch-job-content')` on new job |
| `worker/lib/url-normalize.mjs` | Reused | `HOST_RULES` + new exported `isHostAllowed` |
| `api/internal/jobs/service.go` | Modify | `allowedHostPatterns`/`lookupAllowedHost` mirror + enqueue gate in `AddManual` |
| `db/queries/jobs.sql` + `api/internal/db/jobs.sql.go` | New (dead) | `UpdateJobScrapedContent` — regenerated but unwired (Decision 5) |

## Open Questions / Follow-ups

- **O-1 (operational, unresolved)** — `generate-pdf` and `fetch-job-content`
  are two Chromium consumers sharing `shm_size:1gb` (`docker-compose.yml`).
  Both at `teamSize:3`. No code change addresses combined memory pressure; it
  is a monitored operational risk, not a shipped mitigation. Upgrade path:
  lower a team size or raise `shm_size` if OOM appears.
- **F-1 (drift risk)** — the SSRF allowlist is duplicated across Go and JS
  (Decision 3). Kept in sync by hand. Follow-up only: a shared config both
  runtimes parse, if drift ever bites. Not scheduled.
- **F-2 (dead code)** — remove or wire `UpdateJobScrapedContent` sqlc query
  (Decision 5). Currently unused in the API.
- **F-3 (TEST GAP — for `tasks.md` to pick up as a documented gap, NOT to
  build now)** — per `spec.md` deviation #4, there is ZERO automated coverage
  for: `worker/jobs/fetch-job-content.mjs`, `worker/shared/fetch-page.mjs`,
  `isHostAllowed`, the `ingest-email` enqueue call, and `AddManual`'s
  enqueue-on-allowlisted-host behavior. Only pure helpers `detectPlatform` and
  `lookupAllowedHost` have Go unit tests (`service_test.go`). The proposal's
  Success Criteria ("SSRF gate proven") is therefore not currently backed by a
  test. `tasks.md` MUST record this as an explicit outstanding gap; per the
  current scope decision, no tests are to be written in this backfill.
