# Exploration — `ingest-cv` (Conversational CV ingestion)

> Phase: explore · Status: complete · Artifact store: openspec
> Mirror in engram: `sdd/ingest-cv/explore` (obs #236)

## Intent

Add a conversational CV ingestion flow: a user pastes/uploads their raw CV, an
LLM (Claude) extracts it into `users.cv_markdown` + `users.profile_json`, with
zero manual form-filling. This unblocks the MVP — today there is **no working
path** to get a CV into the columns the evaluation and PDF pipelines depend on.

## Current State (verified against actual code)

**The gap is real and confirmed.** Nothing ever writes `users.cv_markdown` /
`users.profile_json` except the OAuth upsert path (always defaults to null/`{}`).

- `worker/lib/prompt.mjs:21-26` — `buildEvaluationPrompt` reads
  `SELECT cv_markdown, profile_json FROM users WHERE id = $1::uuid LIMIT 1`
  (confirmed lines 21-26).
- `worker/jobs/pdf.mjs:166-172` — also reads the same two columns to build CV PDF HTML.
- `db/queries/users.sql:17-27` — `UpdateUserCVMarkdown` (17-21) and
  `UpdateUserProfileJSON` (23-27) exist and are sqlc-generated into
  `api/internal/db/users.sql.go`, but grep confirms **zero call sites** in `api/`
  outside generated code — fully orphaned queries.
- `api/internal/auth/service.go` — `UpsertUser` (line 17) and `GetUserByID`
  (line 73) scan both columns but never write non-null values; DB defaults are
  `NULL` / `'{}'::jsonb` (`db/schema.sql:37-38`).
- `api/internal/cv/handler.go` — `POST /api/cvs` → `CreateCV` (line 136) writes
  to the separate `cvs` table (`title, content_md, is_master`,
  `db/schema.sql:99-109`). **No consumer reads the `cvs` table** (grep-confirmed).
  Dead end for the evaluate/PDF flows.

**RLS is compatible with the proposed write path.** `db/rls.sql:33-35` —
`tenant_users` policy `USING (id = current_setting('app.current_user_id', true)::uuid)`.
Since `users.id` IS the tenant key, a worker call
`tenantQuery(userId, 'UPDATE users SET cv_markdown=$1, profile_json=$2 WHERE id=$3', ...)`
passes RLS cleanly.

## Reference patterns to mirror (read in full before designing)

**Enqueue side (Go API)** — `api/internal/evaluate/handler.go` + `service.go`:
- Handler thin, delegates to `Servicer.EnqueueEvaluation`; maps
  `ErrNotFound`→404, `ErrUsageLimitExceeded`→402 (`evaluate/handler.go:52-63`).
- Service `EnqueueEvaluation` (`evaluate/service.go:49-97`): (1) ownership check,
  (2) usage-limit check vs `usage` table (`freePlanEvalLimit = 5`, line 24),
  (3) marshal payload + `queue.Enqueue(ctx, pool, queue.Job{Name, Data})`.
- `queue.Enqueue` lives in `api/internal/queue/boss.go`.

**Worker job handler** — `worker/jobs/evaluate.mjs`:
- `handleEvaluateJob(job)` destructures `job.data`, builds prompt, calls
  Anthropic, parses with a **parse-error guard** (`parseEvaluationResponse`,
  lines 14-71) that NEVER throws — persists `{ parse_error: true, raw }` so the
  row is never lost (T-58). Replicate this guard for `ingest-cv`.
- **Correction**: `evaluate.mjs` does **NOT** emit NOTIFY. Only `scan.mjs` does.
  So `scan.mjs` (not `evaluate.mjs`) is the reference for the WS-emission half.

**NOTIFY mechanism** — `worker/lib/progress.mjs:15-24`, single helper:
```js
export async function notify(pgClient, scanRunId, event, data) {
  const payload = JSON.stringify({ event, scan_run_id: scanRunId, ts: new Date().toISOString(), data })
  await pgClient.query(`SELECT pg_notify('scan_progress', $1)`, [payload])
}
```
Reusable, but hardcoded to the `scan_progress` channel + `scan_run_id` field.
Only caller is `worker/jobs/scan.mjs` (lines 73,102,147,172,208 + `finalizeScanRun`).

**WS path (Go API)** — more coupled to "scan" than first assumed:
- `api/internal/ws/listener.go:17-22` — `notifyPayload` has hardcoded
  `ScanRunID string \`json:"scan_run_id"\``; `listener.go:65` does
  `conn.Exec(ctx, "LISTEN scan_progress")` (channel name hardcoded).
- `api/internal/ws/hub.go` — `Hub` keyed generically by `uuid.UUID`
  (`connections map[uuid.UUID]map[string]chan []byte`) — **not** scan-specific.
- `api/internal/ws/handler.go:31` — `ScanProgressHandler(hub, jwtSecret)` reads
  `token` + `scan_run_id` query params; route `r.Get("/ws/scan", ...)` in
  `api/cmd/api/main.go:126`. Only WS route in the app.
- `web/hooks/useScanProgress.ts` — fully scan-specific: hardcoded `/ws/scan` URL,
  status enum `idle|connecting|scanning|completed|partial|error`, literal event
  matching `scan.completed` / `scan.started` (lines 50, 68-73).
- Real `scan_runs` table (`db/schema.sql:112-121`) backs the correlation ID;
  `api/internal/scan/service.go:49` does `q.InsertScanRun(ctx, userID)`.
  **No equivalent table for ingest runs exists today.**

**Anthropic call** — `worker/lib/anthropic.mjs:19-32` — single `evaluate(systemBlocks, userContent)`,
hardcoded `claude-sonnet-4-6`, `max_tokens: 8000`, `temperature: 0.2`,
`cache_control: { type: 'ephemeral' }` on system blocks. For ingest-cv, add a new
exported `ingestCV(...)` to this same file reusing the client singleton.

**Worker registration** — `worker/index.mjs:24-27` registers `evaluate-job` with
`teamSize: 5`; register `ingest-cv` similarly (`teamSize: 5` is fine, not a hot path).

**Test patterns** — `worker/tests/jobs/evaluate.test.mjs:1-19`: `vi.mock('../../lib/db.mjs')`,
`vi.mock('../../lib/prompt.mjs')`, `vi.mock('../../lib/anthropic.mjs')`, then dynamic
`await import('../../jobs/evaluate.mjs')` after mocks. New
`worker/tests/jobs/ingest-cv.test.mjs` follows this shape. Go side uses
`testify/mock` against the local per-package `Servicer` interface.

## `profile_json` shape — investigation result

**No `web/` consumer reads `profile_json` or `cv_markdown`** (grep: zero matches).
Only consumers: `worker/lib/prompt.mjs` (interpolates raw JSON into eval prompt)
and `worker/jobs/pdf.mjs` (`profile.name || profile.full_name` for PDF label,
`pdf.mjs:25`). Therefore the shape is **free to define** — no backward-compat
constraint — with ONE hard requirement: `pdf.mjs:25` reads a top-level `name`/
`full_name` key.

Recommended schema (per career-ops `config/profile.yml` precedent):
```json
{
  "candidate": {
    "full_name": "string", "email": "string", "phone": "string",
    "location": "string", "linkedin": "url", "github": "url", "portfolio_url": "url"
  },
  "target_roles": {
    "primary": ["string"],
    "archetypes": [{ "name": "string", "level": "string", "fit": "string" }]
  },
  "salary_target": { "min": "number", "max": "number", "currency": "string" },
  "narrative": "string"
}
```
**Caveat:** `pdf.mjs:25` reads `profile.name || profile.full_name` at the TOP level,
not `profile.candidate.full_name`. If we nest under `candidate`, `pdf.mjs` must be
updated to `profile.candidate?.full_name` as part of this change (small, in-scope).

## Proposed approach (verified compatible)

`POST /api/cv/ingest {raw_cv}` (new Go route) → ownership/usage check →
`queue.Enqueue(..., Name: "ingest-cv")` → worker `handleIngestCV(job)` → build
prompt → `ingestCV()` in `anthropic.mjs` → parse with guard mirroring
`parseEvaluationResponse` → `tenantQuery(userId, 'UPDATE users SET cv_markdown=$1, profile_json=$2 WHERE id=$3', ...)`
→ `notify(client, runId, 'ingest.completed', {...})` → `ws/listener.go` LISTEN →
`hub.Broadcast` → browser WS. Respects every CLAUDE.md invariant.

## Open questions — options & tradeoffs (decide in `sdd-propose`)

**1. WS correlation key: generalize `scan_run_id` or reuse as-is?**
- **A — Reuse** `scan_run_id` field for ingest runs (smallest change, zero Go ws/
  changes). Con: misleading field name; mixed event types on one `scan_progress`
  channel (works structurally — both share one Postgres channel — but poor hygiene).
- **B — Generalize** field/channel naming across `progress.mjs`, `listener.go`,
  `handler.go`, `useScanProgress.ts` (hub.go already generic). Pro: clean for future
  job types. Con: touches 5 files / both services, regression risk on scan tests.
- **Recommendation:** scoped B — **keep the `scan_progress` channel name** (renaming
  the Postgres channel is a deploy-coordination risk: API+worker must change
  atomically), but generalize the JSON field to `run_id`, and add a **new** worker
  route + **new** web hook (`useJobProgress`) rather than retrofitting `useScanProgress`.

**2. NOTIFY mechanism** — confirmed `worker/lib/progress.mjs` reusable; only the
`scan_run_id` field name is baked in (`progress.mjs:18`). ingest can call the same
`notify()` (with the field rename if Option B).

**3. Raw CV input — text vs PDF.** Worker has **no PDF text-extraction lib**
(`package.json`: only `playwright`, for HTML→PDF render, not reading PDFs).
**Recommend text-first for MVP**: accept plain text/markdown in the body. PDF upload
is a fast-follow (add a PDF→text pre-step) without changing the pipeline shape.

**4. One Claude call vs two.** `parseEvaluationResponse` precedent favors **one call,
structured parsing with a fallback sentinel**. Recommend one call returning both
blocks in a delimited format + the "never lose the row" guard (on parse failure,
still persist raw `cv_markdown` + `profile_json: { parse_error: true }`). Two calls
double latency/cost — not justified unless single-call proves unreliable in tests.

**5. Conversational editing** ("add this project", "change my salary"). Nothing in
the codebase supports any chat/follow-up turn (no conversation state/threads).
**Recommend explicitly OUT OF SCOPE** — separate follow-up change
(`edit-cv-conversational`). MVP value is "get CV in once"; scope creep here blows
the 400-line PR budget (new tables, endpoint, worker logic, UI chat).

**6. Usage limits / plan gating.** `evaluate/service.go:24,65-77` —
`freePlanEvalLimit = 5` vs `usage` table for current month → 402. `usage` has
`evaluations_count` + `pdfs_count` (`db/schema.sql:124-131`) but **no
`ingestions_count`**. Recommend gating consistently (`freePlanIngestLimit`) → small
migration adding a column (don't reuse `evaluations_count` — different action).

## Risks / unknowns

- **WS coupling**: scan-specific hardcoding (`scan_progress`, `scan_run_id`,
  `/ws/scan`, event-name matching in `useScanProgress.ts`). Needs an explicit
  decision (Q1), not a drop-in reuse.
- **No `cv_ingestions` table** — unlike `scan_runs`, nothing to `INSERT ... RETURNING id`.
  Proposal must decide: add a minimal `cv_ingestions` table (id, user_id, status,
  started_at, finished_at) enabling status-polling fallback, vs a bare UUID (cheaper,
  no `GET /api/cv/ingest/:id` polling fallback).
- **profile_json shape collision**: `pdf.mjs:25` depends on top-level `name`/
  `full_name`. Nested schema must update that line or silently mislabel the PDF.
- **`evaluate.mjs` is not a NOTIFY reference** — cite `scan.mjs` + `progress.mjs`.
- **No ingestion usage-quota column** — minor migration if gating desired (DB
  migration → review/rollback considerations).
- **Task-ID drift**: existing commits use "T-NN" IDs; no canonical registry found.
  Tasks phase should establish the next ID range.

## Ready for Proposal — YES

Key decisions for `sdd-propose` to make explicitly: (1) WS correlation-key strategy
(A vs B; recommend scoped B), (2) add `cv_ingestions` table or bare UUID, (3) one-call
vs two-call Claude (recommend one-call), (4) usage-quota column, (5) scope boundary
excluding conversational editing as fast-follow.
