# Tasks: Gmail Job Ingestion

> Task ID range: **T-218 .. T-251** (contiguous, fresh range — highest existing ID is `T-215`
> in `sdd/web-feature-folders/tasks`; T-216/T-217 reserved as optional/deferred tasks in that change)

---

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~1 700–2 000 (impl + tests; 4 layers: DB/Go/Worker/Web) |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 → DB + OAuth token · PR 2 → Go enqueue · PR 3 → Worker · PR 4 → Web + LLM fallback |
| Delivery strategy | chained PRs (resolved 2026-07-01) |
| Chain strategy | stacked-to-main |

Decision needed before apply: No (resolved — chained PRs, stacked-to-main)
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | Migration 006 + sqlc + Go incremental consent OAuth | PR 1 | Base: main. **Blocking prerequisite** — nothing else works without this. |
| 2 | Go `emailingest/` package + pg-boss queue registration | PR 2 | Base: PR 1 branch. API fully wired after this. |
| 3 | Worker: gmail.mjs + url-normalize + parsers + ingest-email handler | PR 3 | Base: PR 2 branch. Largest slice (~700 impl + test lines). |
| 4 | Web "Connect Gmail" + "Sync email alerts" UI + LLM fallback | PR 4 | Base: PR 3 branch. Flag-gated fallback; 0 tokens default. |

---

## PR 1 — DB Foundation + OAuth Token Persistence

Blocking prerequisite. `users.google_refresh_token` + `email_ingest_runs` + the
incremental consent endpoint must land before any other unit is meaningful.

**Sequencing**: T-218 → T-219 → T-220 → T-221 → T-222 → T-223 → T-224 → T-225 → T-226 → T-227 (strictly sequential)

| ID | Type | Description | Files |
|----|------|-------------|-------|
| T-218 | test | pgTAP: assert `users.google_refresh_token` nullable column exists; assert `email_ingest_runs` has `FORCE ROW LEVEL SECURITY` and a cross-tenant SELECT returns 0 rows — spec scenarios "RLS scoped" + "first-time Gmail connection" | `db/tests/gmail_ingestion_rls.test.sql` (new) |
| T-219 | impl | `006_gmail_ingestion.sql`: `ALTER TABLE users ADD COLUMN google_refresh_token text`; `CREATE TABLE email_ingest_runs` (id, user_id, status CHECK running/completed/partial/error, new_jobs, errors_json jsonb, started_at, finished_at) + index + `ENABLE`/`FORCE ROW LEVEL SECURITY` + NULLIF tenant policy + `GRANT app_user` → makes T-218 green | `db/migrations/006_gmail_ingestion.sql` (new) |
| T-220 | impl | Mirror `tenant_email_ingest_runs` policy into `db/rls.sql` (bootstrap source of record; fresh `docker compose up` must match a migrated DB) | `db/rls.sql` |
| T-221 | impl | `db/queries/users.sql`: add `UpdateUserGoogleRefreshToken :one`; new `db/queries/email_ingest_runs.sql`: `InsertEmailIngestRun :one`, `GetEmailIngestRunByID :one`, `UpdateEmailIngestRunStatus`, `UpdateEmailIngestRunNewJobs`, `AppendEmailIngestRunError`; run `cd db && sqlc generate` | `db/queries/users.sql`, `db/queries/email_ingest_runs.sql` (new), `api/internal/db/*` (generated) |
| T-222 | verify | `make test-rls` — T-218 green; `google_refresh_token` col present + cross-tenant deny confirmed | n/a |
| T-223 | test | `api/internal/auth/service_test.go`: `PersistGmailRefreshToken(ctx, userID, token)` upserts on first call; second call replaces existing token — spec scenarios "first-time Gmail connection" + "re-consent replaces existing token" | `api/internal/auth/service_test.go` |
| T-224 | impl | `api/internal/auth/service.go`: add `PersistGmailRefreshToken` via `platform.WithTenantTx` + `UpdateUserGoogleRefreshToken` → makes T-223 green | `api/internal/auth/service.go` |
| T-225 | test | `api/internal/auth/gmail_test.go`: `GET /auth/google/gmail` redirect URL includes `gmail.readonly` scope + `prompt=consent` + `access_type=offline`; `GET /auth/google/gmail/callback` validates state, calls `PersistGmailRefreshToken`, does NOT issue app JWTs — spec scenarios "scope at incremental consent" + "read-only privacy" | `api/internal/auth/gmail_test.go` (new) |
| T-226 | impl | `api/internal/auth/gmail.go`: `NewGmailOAuthConfig(cfg)`, `HandleGmailOAuth` (authenticated, signed state with userID, `AccessTypeOffline`, `SetAuthURLParam("prompt","consent")`), `HandleGmailOAuthCallback` (validate state → exchange code → `PersistGmailRefreshToken` → redirect; no app JWTs); register `GET /auth/google/gmail` + `GET /auth/google/gmail/callback` in `api/cmd/api/main.go` → makes T-225 green | `api/internal/auth/gmail.go` (new), `api/cmd/api/main.go` |
| T-227 | verify | `cd api && go test ./internal/auth/... -count=1` — T-223 + T-225 green | n/a |

---

## PR 2 — Go Enqueue + Queue Registration

**Depends on**: PR 1 (sqlc-generated `EmailIngestRun` type, `WithTenantTx`, auth service).

**Sequencing**: T-228 → T-229 → T-230 → T-231 → T-232 → T-233 (strictly sequential)

| ID | Type | Description | Files |
|----|------|-------------|-------|
| T-228 | test | [x] `api/internal/emailingest/service_test.go`: `TriggerIngest` returns `ErrGmailNotConnected` when `google_refresh_token` NULL; returns `(run_id, nil)` + asserts `InsertEmailIngestRun` called + `queue.Enqueue("ingest-email",{user_id,ingest_run_id})` called on raw pool when token present — spec scenarios "ingest attempted without Gmail token" + "successful trigger" | `api/internal/emailingest/service_test.go` (new) |
| T-229 | impl | [x] `api/internal/emailingest/service.go`: `Servicer` interface (`TriggerIngest`, `GetIngestRun`); `TriggerIngest` with `platform.WithTenantTx` → read token (null→`ErrGmailNotConnected`), `InsertEmailIngestRun`, after-commit `queue.Enqueue` on raw pool; `GetIngestRun` → `GetEmailIngestRunByID` → makes T-228 green | `api/internal/emailingest/service.go` (new) |
| T-230 | test | [x] `api/internal/emailingest/handler_test.go` (testify/mock on Servicer): `POST /api/email-ingest` → 422 `{"error":"gmail_not_connected"}` / 202 `{"ingest_run_id":"<uuid>"}`; `GET /api/email-ingest-runs/{id}` → 200 run shape / 404 non-owner | `api/internal/emailingest/handler_test.go` (new) |
| T-231 | impl | [x] `api/internal/emailingest/handler.go`: `Handler` + `RegisterRoutes` (`POST /api/email-ingest`, `GET /api/email-ingest-runs/{id}`); wire `emailingest.NewService` + `NewHandler` + `RegisterRoutes` in `api/cmd/api/main.go` alongside scan → makes T-230 green | `api/internal/emailingest/handler.go` (new), `api/cmd/api/main.go` |
| T-232 | impl | [x] `worker/scripts/install-pgboss.mjs`: add `'ingest-email'` to `QUEUE_NAMES` — **footgun**: missing entry = silent job loss; `boss.createQueue` must run before Go enqueues | `worker/scripts/install-pgboss.mjs` |
| T-233 | verify | [x] `cd api && go test ./internal/emailingest/... -count=1` | n/a |

---

## PR 3 — Worker: Gmail Lib + Parsers + Ingest Handler

**Depends on**: PR 1 (token in DB), PR 2 (queue registered, `ingest-email` queue exists).

**Sequencing within sub-phases**; sub-phases can be reviewed independently but implemented in order:
gmail.mjs (T-234→T-235) → url-normalize (T-236→T-237) → parsers (T-238→T-239→T-240) → handler (T-241→T-242→T-243→T-244→T-245)

| ID | Type | Description | Files |
|----|------|-------------|-------|
| T-234 | test | [x] `worker/__tests__/lib/gmail.test.mjs`: `decodeMessage` on nested multipart/alternative + single-part base64url fixtures → `{from,subject,html,text}`; stub `fetchJson` → `getAccessToken` returns access token string; SSRF allowlist rejects off-list host with error — spec scenarios "read-only privacy" + "Gmail query scoped to allowlist" | `worker/__tests__/lib/gmail.test.mjs` (new), fixture MIME payload files |
| T-235 | impl | [x] `worker/lib/gmail.mjs`: `getAccessToken(refreshToken)` (POST `https://oauth2.googleapis.com/token` `x-www-form-urlencoded` via `fetchJson` from `providers/_http.mjs`), `listMessages(token,q,max=50)`, `getMessage(token,id)`, `decodeMessage(payload)` (recursive `parts[]` walk, `Buffer.from(data,'base64url').toString('utf8')`); SSRF host allowlist `{oauth2.googleapis.com, gmail.googleapis.com}` + `redirect:'error'`; reads `GOOGLE_CLIENT_ID`/`GOOGLE_CLIENT_SECRET` from env → makes T-234 green | `worker/lib/gmail.mjs` (new) |
| T-236 | test | [x] `worker/__tests__/lib/url-normalize.test.mjs`: table test — raw LinkedIn/Indeed/Computrabajo/Bumeran URLs with tracking params → canonical forms; fallback URL strips `utm_*`/`gclid`/tracking params — spec scenario "duplicate job" (same job, two tracked links dedups) | `worker/__tests__/lib/url-normalize.test.mjs` (new) |
| T-237 | impl | [x] `worker/lib/url-normalize.mjs`: `normalizeJobUrl(platform, rawUrl)` pure function; LinkedIn→`https://www.linkedin.com/jobs/view/{id}` (drop all query); Indeed→`https://www.indeed.com/viewjob?jk={id}`; computrabajo/bumeran→strip `utm_*`+tracking keep path; fallback→strip `utm_*`/`gclid`/known email-tracking params → makes T-236 green | `worker/lib/url-normalize.mjs` (new) |
| T-238 | test | [x] `worker/__tests__/email-parsers/*.test.mjs` (4 test files, one per sender): placeholder fixture HTML per sender → `[{title,company,url}]`; `senderMatch(from)` positive + negative; `allSenders()` returns union of all `senders[]` — spec scenarios "Gmail query scoped to allowlist" + "parsing and job upsert" | `worker/__tests__/email-parsers/{linkedin,computrabajo,bumeran,indeed}.test.mjs` (new), `worker/email-parsers/__fixtures__/*.html` (placeholder) |
| T-239 | impl | [x] `worker/email-parsers/{linkedin,computrabajo,bumeran,indeed}.mjs` (4 modules: `id`, `senders[]`, `senderMatch(from)`, `parse({subject,html,text})→[{title,company,url}]`); `worker/email-parsers/index.mjs`: registry + `getParsers()` + `allSenders()` (single source for `q` filter) → makes T-238 green | `worker/email-parsers/` (5 new files) |
| T-240 | action | [ ] pending-production (placeholder senders/fixtures shipped per design; does not block CI). **Pin PLACEHOLDER sender addresses**: confirm exact `senders[]` values against a real inbox (design candidates: LinkedIn `jobalerts-noreply@linkedin.com`, Computrabajo `no-reply@computrabajo.com`, Bumeran `no-reply@bumeran.com.pe`, Indeed `alert@indeed.com`); replace placeholder `__fixtures__/*.html` with real email samples; re-run T-238 suite to verify → **blocks production reliability; does not block CI** | `worker/email-parsers/{linkedin,...}.mjs`, `worker/email-parsers/__fixtures__/` |
| T-241 | test | [x] `worker/__tests__/jobs/ingest-email.test.mjs`: stub `tenantQuery` + `gmail.mjs` + parser registry; null token → `UPDATE status='error'`, no Gmail calls; revoked token (getAccessToken throws) → status `error` + `token_revoked` in `errors_json`; 2 new + 1 dup + 1 parse-error → status `partial`, correct counts; all succeed → status `completed` — spec scenarios "graceful degradation", "run tracking", "token revoked" | `worker/__tests__/jobs/ingest-email.test.mjs` (new) |
| T-242 | impl | [x] `worker/jobs/ingest-email.mjs`: flat handler payload `{user_id,ingest_run_id}`; reads token → `getAccessToken` → build `q` from `allSenders()` → `listMessages` → per-msg `getMessage`→`decodeMessage`→`senderMatch`→`parse`→`normalizeJobUrl`→upsert `ON CONFLICT (user_id,url) DO NOTHING RETURNING id,(xmax=0) AS is_new`→`notify('ingest.job_found')`; per-msg try/catch append `errors_json` never re-throw; finalize `UPDATE email_ingest_runs SET status=<completed|partial|error>, finished_at=NOW()` + `notify('ingest.completed')` → makes T-241 green | `worker/jobs/ingest-email.mjs` (new) |
| T-243 | impl | [x] `worker/index.mjs`: `import { handleIngestEmail } from './jobs/ingest-email.mjs'` + `registerWorker('ingest-email', handleIngestEmail, { teamSize: 5 })` | `worker/index.mjs` |
| T-244 | impl | [x] `.env.example`: add `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` under worker section with `# never log` comment | `.env.example` |
| T-245 | verify | [x] `cd worker && npm test` — T-234/T-236/T-238/T-241 all green (141 passed, 2 skipped, 0 regressions) | n/a |

---

## PR 4 — Web UI + LLM Fallback

**Depends on**: PR 2 (API endpoints live), PR 3 (worker handles `ingest-email` jobs).

**Sequencing**: T-246 → T-247 → T-248 → T-249 → T-250 → T-251 (strictly sequential)

| ID | Type | Description | Files |
|----|------|-------------|-------|
| T-246 | test | [x] `web/__tests__/...`: "Connect Gmail" button triggers navigation to `/auth/google/gmail`; "Sync email alerts" button calls `POST /api/email-ingest` and polls `GET /api/email-ingest-runs/{id}` displaying `running`/`completed`/`partial`/`error` status — spec scenario "successful trigger" | `web/__tests__/...` (new) |
| T-247 | impl | [x] Web "Connect Gmail" button (link/navigate to `/auth/google/gmail`); "Sync email alerts" button (`POST /api/email-ingest` → poll run status until terminal state); wire into dashboard or settings page → makes T-246 green | `web/` (new component files) |
| T-248 | verify | [x] `cd web && npm test -- --run` | n/a |
| T-249 | test | [x] `worker/__tests__/email-parsers/_llm.test.mjs`: `EMAIL_PARSER_LLM_FALLBACK` unset → 0 LLM calls even when parse returns `[]`; flag `==='true'` + `parsed.length===0` + matched sender → `parseEmailWithLLM` called exactly once; result subject to same upsert + dedup rules — spec scenarios "default run (no flag)" + "LLM fallback enabled" | `worker/__tests__/email-parsers/_llm.test.mjs` (new) |
| T-250 | impl | [x] `worker/email-parsers/_llm.mjs`: `parseEmailWithLLM({subject,html,text})` reusing `lib/anthropic.mjs`; wire `if (process.env.EMAIL_PARSER_LLM_FALLBACK==='true' && parsed.length===0 && matchedSender)` branch in `worker/jobs/ingest-email.mjs`; module not imported when flag unset → guaranteed 0 tokens on default path → makes T-249 green | `worker/email-parsers/_llm.mjs` (new), `worker/jobs/ingest-email.mjs` |
| T-251 | verify | [x] `make test-all` — full suite green: Go (`go test ./...`), worker vitest, web vitest, pgTAP (`make test-rls`) | n/a |

---

## Parallelism Notes

- T-234–T-237 (gmail.mjs + url-normalize) can be worked in parallel by two engineers once PR 2 merges.
- T-238–T-239 (parsers) can run in parallel with T-234–T-237 since both are pure/isolated; `index.mjs` should land last.
- T-240 (pin sender addresses) can start immediately after T-239 but **does not block CI** — placeholder fixtures pass unit tests; it only blocks production correctness.
- PR 3 as a whole is the critical-path bottleneck: the ingest-email handler (T-241–T-242) depends on all prior worker files.
