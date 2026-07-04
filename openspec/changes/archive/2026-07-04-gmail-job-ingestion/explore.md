# Exploration: gmail-job-ingestion

> SDD explore phase. Artifact store: hybrid (this file + Engram `sdd/gmail-job-ingestion/explore`).

## Intent

Ingest job postings from the user's **Gmail job-alert emails** (LinkedIn, Computrabajo, Bumeran, Indeed, …) instead of scraping aggregator sites. Aggregators are unsupported by the 6 ATS providers, and scraping them means ToS violations, IP bans, anti-bot, and fragile HTML. The reframe: those sites already email the user alerts (which the user subscribed to) — read the user's own inbox, parse the postings, and upsert into `jobs`. Covers the local (PE/LatAm) market gap and LinkedIn/Indeed for free.

## Current state (grounding)

- **Worker job registration** (`worker/index.mjs`): 4 jobs via `registerWorker()`. A new `ingest-email` slots in with one call + one handler file.
- **Scan flow** (`worker/jobs/scan.mjs`) — the model to mirror: flat async handler → fetch → provider → upsert `INSERT ... ON CONFLICT (user_id, url) DO NOTHING RETURNING id, (xmax=0) AS is_new` → NOTIFY. All writes via `tenantQuery(userId, …)` (RLS).
- **`jobs` table**: `UNIQUE(user_id, url)` is the dedup key. `platform text` would hold the sender name.
- **Provider pattern** (`worker/providers/*.mjs`): `{ id, fetch(entry, ctx) }`. Email parsers can mirror as `worker/email-parsers/*.mjs`: `{ id, senderMatch(from), parse(subject, html, text) }`.
- **DDD-lite** is only used by evaluate-job. Email ingestion is a fetch-parse-upsert loop → use the **flat handler pattern** (like scan/ingest-cv), not a domain model.

## CRITICAL GAP — Google OAuth token is not persisted

Highest-risk finding, confirmed in code:
- `api/internal/auth/oauth.go:31` — scopes are `["openid","email","profile"]`, no `gmail.readonly`.
- `api/internal/auth/handler.go:50` — `AccessTypeOffline` IS requested (Google would return a refresh token), but (lines 70-86) the token is used only for `GetUserInfo` then **discarded**.
- `api/internal/auth/service.go` `UpsertUser` writes only `email, google_id, name`. `users` table has no token column.

Gmail read requires: (1) add `gmail.readonly` scope, (2) persist the per-user Google **refresh token**, (3) a re-consent path for existing users (incremental auth).

## Approaches compared

**Trigger**: on-demand `POST` (mirrors scan, zero new infra, not automatic) ✅ MVP · pg-boss scheduled polling (automatic, no cron today, N×users calls) → MVP+1 · Gmail Pub/Sub push (real-time, GCP setup, watch expires every 7 days) → later.

**OAuth token storage**: `google_refresh_token TEXT` on `users` (simplest, RLS covers it) ✅ MVP · separate `user_google_tokens` table (cleaner, overkill for MVP). Worker refreshes the token itself via raw `fetchJson` to `oauth2.googleapis.com/token` (no new deps; slight boundary blur, acceptable). Re-consent via a dedicated `/auth/google/gmail` incremental endpoint triggered by a "Connect Gmail" button — avoids forcing all users to re-login.

**Parsing**: deterministic per-sender parsers (0 tokens, brittle) ✅ default for top 4 senders · LLM extraction (robust, costs tokens) behind `EMAIL_PARSER_LLM_FALLBACK=true`. Cost-sensitive default = 0-token path.

**Dedup / URL normalization**: email links are tracking-redirect wrapped; `UNIQUE(user_id,url)` only dedups identical URLs. → `worker/lib/url-normalize.mjs` with per-sender rules (strip tracking params, reconstruct canonical URL) ✅ (no schema change) · follow-redirect (sender-agnostic, extra HTTP + bot risk) · `gmail_message_id` column (exact per-email, but same job from 2 senders still dupes).

**Privacy**: Gmail `q=` filter to read ONLY job-alert emails (`from:(…) subject:(…)`); never request `gmail.modify`/`labels`.

**Where it lives**: worker `ingest-email` job + per-sender parsers; Go API handles enqueue + OAuth. No new service.

## Recommended MVP direction

1. Incremental OAuth endpoint `/auth/google/gmail` → `gmail.readonly` + persist `google_refresh_token` (no disruption to existing login).
2. Migration: `google_refresh_token TEXT` on `users`.
3. Go API: `POST /api/email-ingest` enqueues `ingest-email` (payload `{ user_id, ingest_run_id }`).
4. Worker: read refresh token → exchange for access token (raw fetch) → Gmail API with sender-scoped query → deterministic per-sender parsers → URL normalize → upsert to `jobs` via `tenantQuery` + `ON CONFLICT DO NOTHING`.
5. Run tracking: new `email_ingest_runs` table (cleaner than reusing `scan_runs`, no `company_id` equivalent).

## Risks

1. **CRITICAL** — Gmail refresh token not stored today → migration + auth handler + incremental-consent UX is the primary new infra.
2. Re-consent: Google returns `refresh_token` only on first auth or `prompt=consent` → needs the incremental endpoint.
3. Tracking-link dedup: per-sender normalization rules drift as templates change.
4. Gmail API quota (~10k units/day/user): low with on-demand, moderate with polling.
5. Sender format brittleness → needs a test corpus per sender.
6. Queue registration: `ingest-email` must be added to `worker/scripts/install-pgboss.mjs` or enqueue fails (known project footgun).
7. Gmail returns base64url + multipart MIME (HTML + text) → decode/handle before parsing.

## Verdict

Ready for `sdd-propose`. The proposal must resolve the OAuth token-persistence design first — everything else depends on it.
