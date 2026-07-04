# Design: Gmail Job Ingestion

Ingest job postings from the user's own Gmail job-alert emails (LinkedIn, Computrabajo,
Bumeran, Indeed) and upsert into `jobs`, deduplicated, at 0 LLM tokens by default. This
document is the buildable HOW: the OAuth token flow is resolved first (it is the only
net-new infrastructure), then the worker pipeline that mirrors `scan.mjs` end to end.

Guiding constraints: RLS everywhere, control/data plane split preserved, on-demand only
(no cron), top 4 senders only, deterministic default. No new service, no new npm
dependency, no `jobs` schema change.

---

## Architecture at a glance

```
Web ──POST /api/email-ingest──► Go API (control plane)
                                  │  WithTenantTx(userID):
                                  │    INSERT email_ingest_runs (status='running')
                                  │  after commit (raw pool):
                                  └─ queue.Enqueue("ingest-email", {user_id, ingest_run_id})
                                             │
                                             ▼
                             Worker (data plane)  worker/jobs/ingest-email.mjs
                               1. tenantQuery(user) SELECT google_refresh_token  ← RLS
                               2. fetchJson oauth2.googleapis.com/token  (refresh→access)
                               3. Gmail: messages.list?q=<senders> → messages.get
                               4. decodeMessage() base64url + MIME → {subject, html, text}
                               5. parser.senderMatch(from) → parser.parse() → [{title,company,url}]
                               6. normalizeJobUrl(platform, url)  (dedup key)
                               7. tenantQuery upsert jobs ON CONFLICT DO NOTHING RETURNING is_new
                               8. notify() progress + UPDATE email_ingest_runs
```

Plane rule kept intact except one **conscious, documented** blur: the worker performs
the refresh-token→access-token exchange itself (Decision 2). Go never reads Gmail; the
worker never handles app auth/routing.

---

## Decision 1 (prerequisite): OAuth token persistence

Login today requests `AccessTypeOffline` but **discards** the Google token
(`GoogleCallback` uses it only for `GetUserInfo`), scopes are `openid/email/profile`
only, and `users` has no token column. This must land before anything else works.

### Data model delta

New migration `db/migrations/006_gmail_ingestion.sql`:

```sql
-- 1. Refresh token on users (users already has FORCE ROW LEVEL SECURITY → no rls.sql change)
ALTER TABLE users ADD COLUMN google_refresh_token text;
```

`google_refresh_token` is nullable: existing users have none until they connect Gmail.
Column-on-`users` is the lazy-correct MVP (rejected: separate `user_google_tokens` table
and vault/KMS — YAGNI; RLS already covers `users`).

### sqlc query (control plane)

`db/queries/users.sql` gains:

```sql
-- name: UpdateUserGoogleRefreshToken :one
UPDATE users
SET google_refresh_token = $2
WHERE id = $1
RETURNING *;
```

Read path in Go already exists: `GetUserByID` is `SELECT *`, so the column is picked up
after regeneration. **Regeneration step (must not be skipped):** `cd db && sqlc generate`
— regenerates `api/internal/db/*` structs (`db.User` gets `GoogleRefreshToken`).

### RLS reachability — how the WORKER reads the token (verified)

The `users` policy is `USING (id = current_user_id)`. The worker calls
`tenantQuery(userId, "SELECT google_refresh_token FROM users WHERE id = $1", [userId])`,
which sets `SET LOCAL app.current_user_id = userId`. Because `id == current_user_id`, the
row passes the policy — **the worker can read exactly its own user's token and no other's**.
No `rls.sql` change, no SECURITY DEFINER needed for the read.

### Incremental consent endpoint (avoid forced re-consent)

Adding `gmail.readonly` to the login scope would re-prompt every existing user. Instead a
**separate opt-in OAuth flow**, triggered by a "Connect Gmail" button:

| Route | Purpose |
|-------|---------|
| `GET /auth/google/gmail` | Authenticated. Builds a **second** `oauth2.Config` with scope `gmail.readonly`, `AccessTypeOffline`, and `SetAuthURLParam("prompt", "consent")`. Puts the caller's `userID` into a signed/opaque `state` (also CSRF cookie). Redirects to Google. |
| `GET /auth/google/gmail/callback` | Validates `state`, extracts `userID`, exchanges code, then `WithTenantTx(userID)` → `UpdateUserGoogleRefreshToken(userID, token.RefreshToken)`. Redirects back to web. |

Why `prompt=consent`: Google only re-issues a `refresh_token` on first authorization
**or** when consent is forced. Without it, an already-linked account returns no refresh
token and the worker has nothing to store (Risk 2).

Placement: new file `api/internal/auth/gmail.go` (handler methods on the existing
`auth.Handler`), building `NewGmailOAuthConfig(cfg)` alongside `NewOAuthConfig`. The
callback does NOT issue app JWTs (the user is already logged in) — it only persists the
token and redirects. Register both routes where auth routes are mounted.

> The existing `GoogleCallback` login path is left as-is. We do NOT persist the token
> there (login users may not have granted `gmail.readonly`). Token persistence happens
> only through the incremental flow. This keeps login untouched and scope-minimal.

---

## Decision 2: Token exchange lives in the worker

The worker reads the refresh token (RLS-scoped, above) and exchanges it for a short-lived
access token with **one** `fetchJson` call — no Go `/internal` endpoint (coupling +
latency), no token-in-payload (1h expiry rots for queued backlog).

New `worker/lib/gmail.mjs`:

```js
// getAccessToken(refreshToken) → string
// POST https://oauth2.googleapis.com/token  (application/x-www-form-urlencoded)
//   grant_type=refresh_token&refresh_token=..&client_id=..&client_secret=..
// Uses fetchJson from providers/_http.mjs (timeout, error snippet already handled).
```

New worker env vars (document in `.env.example`): `GOOGLE_CLIENT_ID`,
`GOOGLE_CLIENT_SECRET` (same values the API uses). The credential never leaves the
RLS-scoped worker context and is never logged.

**SSRF/host allowlist** (consistent with providers): `gmail.mjs` asserts the host is in
`{ oauth2.googleapis.com, gmail.googleapis.com }` and `protocol === 'https:'` before every
call, and uses `redirect: 'error'` (same guard style as `greenhouse.mjs`).

---

## Decision 3: Gmail API read + MIME decode

New `worker/lib/gmail.mjs` (same file) exposes:

| Function | Call | Notes |
|----------|------|-------|
| `listMessages(accessToken, q, max=50)` | `GET gmail/v1/users/me/messages?q=<q>&maxResults=<max>` | Returns `[{id}]`. `q` restricts to known senders only (privacy). |
| `getMessage(accessToken, id)` | `GET gmail/v1/users/me/messages/{id}?format=full` | Returns full payload incl. headers + parts. |
| `decodeMessage(payload)` | pure | Walks `payload.parts[]` recursively (handles nested `multipart/alternative`), decodes `body.data` via `Buffer.from(data, 'base64url').toString('utf8')`, returns `{ from, subject, html, text }`. |

Auth header: `Authorization: Bearer <accessToken>`.

`q` filter (privacy — never reads full inbox), built from the sender registry:

```
from:(jobalerts-noreply@linkedin.com OR no-reply@computrabajo.com OR
      no-reply@bumeran.com.pe OR alert@indeed.com) newer_than:14d
```

> **PLACEHOLDER sender addresses** — pin exact values against a real inbox during spec.
> Candidates: LinkedIn `jobalerts-noreply@linkedin.com` / `jobs-noreply@linkedin.com`;
> Computrabajo `no-reply@computrabajo.com`; Bumeran `no-reply@bumeran.com.pe`; Indeed
> `alert@indeed.com` / `donotreply@match.indeed.com`. The `q` string is derived from the
> parser registry's `senders`, so there is a single source of truth (Decision 5).

---

## Decision 4: Worker job `worker/jobs/ingest-email.mjs`

Flat handler mirroring `scan.mjs` (NOT DDD-lite). Payload `{ user_id, ingest_run_id }`.

Flow (per-step error handling, graceful degrade, never re-throw — NFR-07 pattern):

1. `tenantQuery` → read `google_refresh_token`. If null → `UPDATE email_ingest_runs SET
   status='error'`, notify, return (user hasn't connected Gmail).
2. `getAccessToken(refreshToken)`.
3. Build `q` from parser registry senders → `listMessages`.
4. For each message: `getMessage` → `decodeMessage` → match a parser by `senderMatch(from)`
   → `parser.parse({subject, html, text})` → `[{title, company, url}]`.
5. `normalizeJobUrl(parser.id, url)` on each (Decision 6).
6. Upsert reusing the **existing** jobs contract verbatim:
   ```sql
   INSERT INTO jobs (user_id, title, company, url, platform, status, received_at)
   VALUES ($1,$2,$3,$4,$5,'new',NOW())
   ON CONFLICT (user_id, url) DO NOTHING
   RETURNING id, (xmax = 0) AS is_new
   ```
   `platform` = `parser.id`. No `jobs` schema change.
7. `notify(client, ingest_run_id, 'ingest.job_found', {...})` for new rows (mirrors
   `scan.job_found`); accumulate `newCount`.
8. Finalize: `UPDATE email_ingest_runs SET status=<completed|partial>, new_jobs=..,
   finished_at=NOW()` + `notify('ingest.completed', {...})`.

Per-message failures are caught, appended to `errors_json`, and skipped — one bad email
never aborts the run.

**Registration (two footgun-prone spots — both mandatory):**
- `worker/index.mjs`: `import { handleIngestEmail } from './jobs/ingest-email.mjs'` +
  `await registerWorker('ingest-email', handleIngestEmail, { teamSize: 5 })`.
- `worker/scripts/install-pgboss.mjs`: add `'ingest-email'` to `QUEUE_NAMES`. **Missing
  this = silent job loss** — `boss.createQueue` must have run before Go enqueues.

---

## Decision 5: Per-sender parser contract

`worker/email-parsers/*.mjs` — one per sender, mirroring the provider registry pattern.

```js
export default {
  id: 'linkedin',                       // becomes jobs.platform
  senders: ['jobalerts-noreply@linkedin.com'],  // feeds q= AND senderMatch
  senderMatch(from) { return this.senders.some(s => from.toLowerCase().includes(s)) },
  parse({ subject, html, text }) {
    // → [{ title, company, url }]   (url is raw; normalization happens in the handler)
  },
}
```

Registry: `worker/email-parsers/index.mjs` loads the 4 modules into a lookup and exports
`getParsers()` + `allSenders()` (single source for the `q` filter). Mirrors the
`PROVIDER_REGISTRY` load in `scan.mjs`.

v1 senders: **LinkedIn, Computrabajo, Bumeran, Indeed** (top LatAm/Peru alerts). More
senders = YAGNI until these prove out.

---

## Decision 6: URL normalization for dedup

`worker/lib/url-normalize.mjs` — pure function, per-sender canonicalization so the same
job from two tracking-wrapped links dedups against the existing `UNIQUE(user_id, url)`.

```js
// normalizeJobUrl(platform, rawUrl) → string
// Per-sender rule shape:
//   { hostAllow: Set<string>, canonical(url: URL) => string }
```

Rule sketch (verify against real emails in spec/tests):

| platform | rule |
|----------|------|
| linkedin | extract `/jobs/view/{id}`, drop all query → `https://www.linkedin.com/jobs/view/{id}` |
| indeed | keep only `jk` param → `https://www.indeed.com/viewjob?jk={id}` |
| computrabajo | strip `utm_*` + tracking params, keep path |
| bumeran | strip `utm_*` + tracking params, keep path |

Fallback (unknown/no rule): strip `utm_*`, `gclid`, and known email-tracking params, keep
scheme+host+path. Rejected: follow-redirect (extra HTTP, bot-detection) and content-hash
(complexity). Pure function → trivially unit-testable.

---

## Decision 7: Run tracking table `email_ingest_runs`

Mirrors `scan_runs` minus `company_id`-specific fields. Added to migration `006`:

```sql
CREATE TABLE email_ingest_runs (
  id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status      text        NOT NULL DEFAULT 'running'
                            CHECK (status IN ('running','completed','partial','error')),
  new_jobs    integer     NOT NULL DEFAULT 0,
  errors_json jsonb       NOT NULL DEFAULT '[]',
  started_at  timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz
);
CREATE INDEX idx_email_ingest_runs_user ON email_ingest_runs(user_id, started_at DESC);

ALTER TABLE email_ingest_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE email_ingest_runs FORCE  ROW LEVEL SECURITY;
CREATE POLICY tenant_email_ingest_runs ON email_ingest_runs
  USING      (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE, DELETE ON email_ingest_runs TO app_user;
```

Status vocabulary **reuses scan's**: `running` (default) → `completed` / `partial` (some
per-email errors) / `error` (no token / total failure). Also mirror the policy addition
into `db/rls.sql` for schema-of-record consistency.

sqlc queries `db/queries/email_ingest_runs.sql` (mirror `scan_runs.sql`):
`InsertEmailIngestRun :one`, `GetEmailIngestRunByID :one`, `UpdateEmailIngestRunStatus`,
`UpdateEmailIngestRunNewJobs`, `AppendEmailIngestRunError`. The worker uses raw
`tenantQuery` (like `scan.mjs`); these sqlc queries serve the Go read/insert path.
**Run `cd db && sqlc generate` after adding queries.**

---

## Decision 8: Go enqueue path `api/internal/emailingest/`

New hexagonal package mirroring `api/internal/scan/` exactly.

- `handler.go`: `Servicer` interface (`TriggerIngest(ctx, userID) (uuid.UUID, error)`,
  `GetIngestRun(ctx, userID, id)`), `RegisterRoutes` → `POST /api/email-ingest` and
  `GET /api/email-ingest-runs/{id}`. Reuse the `middleware.GetUserID` + `writeJSON/writeError`
  shape from scan's handler.
- `service.go`: `TriggerIngest` runs `WithTenantTx(userID)` → `InsertEmailIngestRun`, then
  **after commit** (pgboss has no RLS, stays on raw pool) `queue.Enqueue("ingest-email",
  {user_id, ingest_run_id})`. Single enqueue (unlike scan's per-company loop).
- Wire `NewService` + `NewHandler` + `RegisterRoutes` where scan is wired in `main.go`.

Payload struct: `{ UserID uuid.UUID; IngestRunID uuid.UUID }` (json: `user_id`,
`ingest_run_id`).

---

## Decision 9: LLM fallback (off by default, 0-token default path)

Hook is a single isolated branch in `ingest-email.mjs`, so the default path stays
deterministic and token-free:

```js
if (process.env.EMAIL_PARSER_LLM_FALLBACK === 'true' && parsed.length === 0 && matchedSender) {
  parsed = await parseEmailWithLLM({ subject, html, text })  // worker/email-parsers/_llm.mjs
}
```

Reuses `worker/lib/anthropic.mjs`. When the flag is unset (default), `_llm.mjs` is never
imported/called → guaranteed 0 tokens. Rejected: LLM-first parsing (ongoing cost; scan is
0 tokens). This is wired but disabled; enabling it is a config-only change.

---

## File-level responsibility map

| File | New/Edit | Responsibility |
|------|----------|----------------|
| `db/migrations/006_gmail_ingestion.sql` | new | `users.google_refresh_token`, `email_ingest_runs` + RLS |
| `db/rls.sql` | edit | mirror `tenant_email_ingest_runs` policy (schema of record) |
| `db/queries/users.sql` | edit | `UpdateUserGoogleRefreshToken` |
| `db/queries/email_ingest_runs.sql` | new | insert/get/update run queries |
| `api/internal/db/*` | generated | `sqlc generate` output — DO NOT hand-edit |
| `api/internal/auth/gmail.go` | new | `NewGmailOAuthConfig`, `/auth/google/gmail(+/callback)` |
| `api/internal/auth/service.go` | edit | helper to persist refresh token via `WithTenantTx` |
| `api/internal/emailingest/handler.go` | new | routes + Servicer |
| `api/internal/emailingest/service.go` | new | run insert + enqueue |
| `api/cmd/.../main.go` (wiring) | edit | mount auth gmail routes + emailingest routes |
| `worker/lib/gmail.mjs` | new | token exchange, list/get, `decodeMessage` |
| `worker/lib/url-normalize.mjs` | new | `normalizeJobUrl(platform, rawUrl)` |
| `worker/email-parsers/{linkedin,computrabajo,bumeran,indeed}.mjs` | new | per-sender parse |
| `worker/email-parsers/index.mjs` | new | registry + `allSenders()` |
| `worker/email-parsers/_llm.mjs` | new | fallback (flag-gated) |
| `worker/jobs/ingest-email.mjs` | new | flat handler (mirrors scan.mjs) |
| `worker/index.mjs` | edit | `registerWorker('ingest-email', ...)` |
| `worker/scripts/install-pgboss.mjs` | edit | add `'ingest-email'` to `QUEUE_NAMES` |
| `.env.example` (worker) | edit | `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` |
| `web/` | edit | "Connect Gmail" + "Sync email alerts" buttons |

---

## Testing seams

| Unit | Seam | Test |
|------|------|------|
| Parsers | pure `parse({subject,html,text})` | fixture corpus `worker/email-parsers/__fixtures__/*.html` → vitest table |
| URL normalize | pure `normalizeJobUrl` | table test of raw→canonical per sender |
| MIME decode | pure `decodeMessage(payload)` | base64url + nested multipart fixtures |
| Token exchange / Gmail read | `gmail.mjs` fns | inject/stub `fetchJson`; assert host allowlist rejects off-list URLs |
| ingest-email handler | mock `tenantQuery` + `gmail.mjs` | mirror existing scan.mjs tests |
| Go emailingest service | mirror scan service test | run insert + enqueue |
| RLS | pgTAP | worker reads only own `users.google_refresh_token`; `email_ingest_runs` cross-tenant deny |

---

## Build order (dependency-correct)

1. Migration 006 + `sqlc generate` (foundation; nothing works without the column/table).
2. Go incremental-consent OAuth (`gmail.go`) + persist token → verify a real refresh token lands in `users`.
3. `worker/lib/gmail.mjs` (token exchange + read + decode) — testable in isolation.
4. Parsers + `url-normalize.mjs` (pure, fixture-driven).
5. `ingest-email.mjs` handler + register in `index.mjs` **and** `install-pgboss.mjs`.
6. Go `emailingest` enqueue path + route wiring.
7. Web buttons.
8. LLM fallback last (flag off).

---

## Risks carried into tasks

1. **OAuth token infra is net-new and blocking** — steps 1-2 must land and be verified
   (real refresh token stored) before the worker path is meaningful.
2. **`prompt=consent` required** — already-linked accounts yield no refresh token
   otherwise; verify end-to-end with a real Google account.
3. **Sender addresses are PLACEHOLDER** — must be pinned against a real inbox in spec;
   they drive both `q=` and `senderMatch`.
4. **Parser + tracking-URL fragility** — LinkedIn template has changed >once; fixture
   corpus is mandatory and will need upkeep.
5. **Queue-registration footgun** — miss `'ingest-email'` in `install-pgboss.mjs` = silent
   job loss.
6. **Worker needs Google client secret** — new worker env vars; keep out of logs.
7. **Gmail quota** — low per-call; on-demand avoids burst; track, don't block.
