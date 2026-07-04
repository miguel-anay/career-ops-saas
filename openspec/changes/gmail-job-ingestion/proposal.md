# Proposal: Gmail Job Ingestion

Ingest job postings from the user's own Gmail job-alert emails (LinkedIn, Computrabajo, Bumeran, Indeed) and upsert them into `jobs`, covering the Peru/LatAm local market that the 6 ATS providers do not reach. We read the user's *subscribed alerts* instead of scraping aggregators — no ToS violation, no anti-bot, no ban risk.

## Intent

- **Problem**: The 6 ATS providers (greenhouse, ashby, lever, recruitee, smartrecruiters, workable) miss the LatAm local market. The obvious fix — scraping LinkedIn/Computrabajo/Bumeran/Indeed — is a ToS/ban/anti-bot dead end.
- **Insight**: The user already receives these listings as job-alert emails they subscribed to. Reading their own inbox (with consent, read-only) is legitimate and reliable.
- **Success**: A user connects Gmail, clicks "Sync email alerts", and net-new jobs from their alert emails appear in the jobs list — deduplicated against existing entries, at zero LLM cost by default.

## Chosen approach

### Decision #1 (prerequisite): Google OAuth token is discarded today — fix it first

The current login flow (`api/internal/auth/`) requests `AccessTypeOffline` but **throws the Google token away** after `GetUserInfo`. Scopes are `openid/email/profile` only; `users` has no token column. Nothing about Gmail works until this is resolved. Resolution:

| Piece | Decision |
|-------|----------|
| Scope | Add `gmail.readonly`. Never request `gmail.modify` or `gmail.labels`. |
| Token storage | New column `google_refresh_token TEXT` on `users`. Already under `FORCE ROW LEVEL SECURITY` — no `rls.sql` change. |
| Consent for existing users | New **incremental-consent** endpoint `POST /auth/google/gmail` with `prompt=consent&access_type=offline`. Existing users are NOT forced to re-login; they opt in by clicking "Connect Gmail". |
| Persist | `GoogleCallback` (and the new Gmail callback) stores `token.RefreshToken` via a new sqlc query. |

Rationale: a column on `users` is the lazy-correct MVP (RLS already covers it, one migration, one query). A separate `user_google_tokens` table is extensible but unwarranted at MVP — YAGNI. Incremental consent is chosen over adding the scope to the main login because it avoids a forced re-consent wall for every existing user.

### Decision #2: Plane boundary — worker does the token exchange

| Plane | Responsibility |
|-------|----------------|
| Go API (control) | OAuth scope + incremental consent flow; persist/read `google_refresh_token`; enqueue `ingest-email` (mirrors scan). **Never** reads Gmail, **never** calls the LLM. |
| Worker (data) | Read refresh token via `tenantQuery` (RLS-scoped); exchange it for an access token with **one** `fetchJson` call to `https://oauth2.googleapis.com/token`; call Gmail API; parse; upsert into `jobs`. |

Rationale: putting the refresh-token→access-token exchange in the worker is a slight boundary blur (worker touching an auth credential), but it is the pragmatic choice. The alternatives are worse: a Go `/internal/gmail-token` endpoint adds inter-service coupling and latency; putting a 1h access token in the job payload rots for any queued backlog. The exchange is a single POST using `_http.mjs`'s existing `fetchJson` — no new dependency, no new service. The credential stays RLS-scoped the whole way.

### Decision #3: Everything else mirrors existing patterns

| Concern | Decision | Mirrors |
|---------|----------|---------|
| Trigger | On-demand `POST /api/email-ingest` → enqueue `ingest-email`. | scan trigger endpoint |
| Worker job | New `ingest-email` handler, **flat** style (fetch→parse→upsert loop). Not DDD-lite. | `worker/jobs/scan.mjs` |
| Parsers | Per-sender `worker/email-parsers/*.mjs`, each `{ id, senderMatch, parse }`. Deterministic, 0 tokens. Top senders only: LinkedIn, Computrabajo, Bumeran, Indeed. | `worker/providers/*.mjs` |
| LLM fallback | Behind `EMAIL_PARSER_LLM_FALLBACK` env, **off by default**. User is cost-sensitive; scan is 0 tokens today. | scan's zero-token posture |
| Dedup | `normalizeJobUrl(platform, rawUrl)` in `worker/lib/url-normalize.mjs` strips per-sender tracking params → feeds existing `INSERT ... ON CONFLICT (user_id,url) DO NOTHING`. No follow-redirect, no schema change to `jobs`. | existing `UNIQUE(user_id,url)` |
| Run tracking | New `email_ingest_runs` table (mirrors `scan_runs`, minus `company_id`). Under RLS. | `scan_runs` |
| Privacy | Gmail `q=` filter restricted to known job-alert senders. Read-only. | — |
| Queue install | Register `ingest-email` in `worker/scripts/install-pgboss.mjs`. | known footgun — miss = silent job loss |

## Scope (in)

- Add `gmail.readonly` scope and persist `google_refresh_token` on `users` (migration + sqlc query).
- Incremental-consent endpoint `POST /auth/google/gmail` + callback that stores the refresh token.
- `POST /api/email-ingest` control-plane endpoint that enqueues `ingest-email`.
- Worker `ingest-email` job: token exchange, Gmail read with `q=` sender filter, parse, upsert.
- Deterministic per-sender parsers for **4 senders**: LinkedIn, Computrabajo, Bumeran, Indeed.
- `normalizeJobUrl` URL-normalization utility for dedup.
- `email_ingest_runs` tracking table under RLS.
- Register `ingest-email` queue in `install-pgboss.mjs`.
- Web: "Connect Gmail" button (OAuth) + "Sync email alerts" trigger button.
- LLM-fallback path wired but **disabled by default** behind an env flag.

## Out of scope

- **Scheduled polling / cron** (`boss.schedule`) — MVP+1. No cron in the system today.
- **Gmail Push / Pub/Sub watch** — major infra (public HTTPS endpoint, 7-day watch renewal).
- **Follow-redirect / content-hash dedup** — normalization covers the known senders.
- **`user_google_tokens` table, vault/KMS credential storage** — column on `users` suffices for MVP.
- **Senders beyond the top 4** — no speculative parsers (YAGNI).
- **`gmail.modify` / `gmail.labels`** — read-only only.
- **App-created Gmail labels** — would require extra scope.

## Key decisions & tradeoffs

| Decision | Chosen | Rejected | Why |
|----------|--------|----------|-----|
| Token store | Column on `users` | Separate table / vault | RLS already covers `users`; one migration. |
| Consent | Incremental `/auth/google/gmail` | Add scope to main login | No forced re-consent for existing users. |
| Token exchange | In worker via `fetchJson` | Go `/internal` endpoint / token-in-payload | No coupling, no 1h-token rot, no new dep. |
| Parsing | Deterministic, LLM flag off | LLM-first | 0 tokens by default; cost-sensitive user. |
| Dedup | Normalize URL | Follow redirect / content hash | Reuses existing UNIQUE; no extra HTTP. |
| Worker style | Flat (scan.mjs) | DDD-lite (evaluate.mjs) | Fetch-parse-upsert loop; DDD is overkill. |

## Risks

1. **OAuth token infra is net-new** — schema migration + sqlc query + auth handler change + re-consent UX. Largest piece; must land before anything Gmail-facing works.
2. **Refresh token only issued once** — Google returns `refresh_token` on first authorization only; `prompt=consent` is required to reissue it on the incremental flow.
3. **Tracking-link dedup drift** — per-sender normalization rules will break when senders change tracking params. Needs a per-sender test corpus.
4. **Parser fragility** — LinkedIn has changed its email template more than once; deterministic parsers rot silently without a fixture corpus.
5. **Queue registration footgun** — forgetting `ingest-email` in `install-pgboss.mjs` = silent job loss.
6. **Email encoding** — Gmail returns `parts[].body.data` base64url-encoded; multipart text/HTML structure varies per sender. Decode before parse.
7. **Gmail API quota** — low per-call cost; on-demand trigger avoids burst. Worth tracking, not blocking.

## Open questions

- Confirm the exact alert-sender addresses per platform (e.g. `jobalert@linkedin.com`) for the `q=` filter and `senderMatch` — to be pinned in spec/design.
- `email_ingest_runs` status vocabulary — reuse scan_runs' set or trim it? (design decision)
