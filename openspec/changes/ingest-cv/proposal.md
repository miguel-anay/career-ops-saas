# Proposal — `ingest-cv` (Conversational CV ingestion)

> Phase: propose · Status: complete · Artifact store: openspec
> Input: `openspec/changes/ingest-cv/explore.md` (engram mirror `sdd/ingest-cv/explore`, obs #236)

A user pastes their raw CV; Claude extracts it into `users.cv_markdown` + `users.profile_json`, with zero form-filling. This is the **MVP-blocking gap**: today nothing writes those two columns, yet the evaluate and PDF pipelines both read them. Ship this and the existing pipelines start producing real output.

## Why

- **The gap is confirmed, not theoretical.** `worker/lib/prompt.mjs:21-26` (evaluate) and `worker/jobs/pdf.mjs:166-172` (PDF) both `SELECT cv_markdown, profile_json FROM users`. Those columns are written **nowhere** except the OAuth upsert, which always defaults them to `NULL` / `'{}'::jsonb` (`db/schema.sql:37-38`).
- **The plumbing already exists but is orphaned.** `UpdateUserCVMarkdown` / `UpdateUserProfileJSON` are sqlc-generated (`api/internal/db/users.sql.go`) with **zero call sites**. The `cvs` table that `POST /api/cvs` writes to has **no reader** — a dead end for evaluate/PDF.
- **Net effect:** every evaluation today runs against an empty profile. This change is the missing first step of the funnel.

## What Changes

**Database (`db/`)**
- New table `cv_ingestions` (id, user_id, status, started_at, finished_at), mirroring `scan_runs` (`db/schema.sql:112-121`). Backs the `INSERT … RETURNING id` correlation ID and a status-polling fallback. Needs `FORCE ROW LEVEL SECURITY` + a tenant policy on `user_id` mirroring `db/rls.sql`.
- Migration: add `ingestions_count INT NOT NULL DEFAULT 0` to the `usage` table (`db/schema.sql:124-131`).
- New sqlc queries in `db/queries/` for `cv_ingestions` (insert-returning-id, update status, get-by-id) → `sqlc generate`.

**Go API (`api/internal/cv/`)**
- New route `POST /api/cv/ingest {raw_cv}` → handler delegates to `Servicer.EnqueueIngest`, mirroring `evaluate/handler.go:52-63` error mapping (`ErrNotFound`→404, `ErrUsageLimitExceeded`→402).
- New route `GET /api/cv/ingest/:id` → reads `cv_ingestions` status (WS-drop polling fallback).
- Service `EnqueueIngest`: (1) ownership/auth via `middleware.GetUserID`, (2) usage check vs `usage.ingestions_count` for current month against `freePlanIngestLimit`, (3) `INSERT cv_ingestions RETURNING id`, (4) `queue.Enqueue(ctx, pool, queue.Job{Name: "ingest-cv", Data})` via `api/internal/queue/boss.go`.

**Worker (`worker/`)**
- `worker/index.mjs` — register `ingest-cv` job (`teamSize: 5`, not a hot path).
- New `worker/jobs/ingest-cv.mjs` — `handleIngestCV(job)`: build prompt → call `ingestCV()` → parse with a **never-throw guard** mirroring `parseEvaluationResponse` (`evaluate.mjs:14-71`) → `tenantQuery(userId, 'UPDATE users SET cv_markdown=$1, profile_json=$2 WHERE id=$3', …)` → mark `cv_ingestions` finished → `notify(client, runId, 'ingest.completed', {…})`.
- New `ingestCV(...)` export in `worker/lib/anthropic.mjs`, reusing the client singleton (same `claude-sonnet-4-6`, `max_tokens`, `temperature`, `cache_control` conventions).
- `worker/lib/progress.mjs:15-24` — rename the NOTIFY JSON field `scan_run_id` → `run_id`. **Keep the `scan_progress` Postgres channel name unchanged** (see Decision 1).

**Web (`web/`)**
- New hook `web/hooks/useJobProgress.ts` (generalized clone of `useScanProgress.ts`) listening for `ingest.*` events on the WS.
- New UI: paste-CV textarea + submit + live status, calling `POST /api/cv/ingest`.

**WS path (`api/internal/ws/`)**
- `listener.go:17-22` — rename `notifyPayload.ScanRunID` (`json:"scan_run_id"`) → `RunID` (`json:"run_id"`). `hub.go` is already generically keyed by `uuid.UUID` — untouched. LISTEN string untouched (channel name stays `scan_progress`).

**PDF fix (`worker/jobs/pdf.mjs`)**
- `pdf.mjs:25` — update `profile.name || profile.full_name` → `profile.candidate?.full_name` to match the new nested `profile_json` schema (Decision 4).

## Explicitly Out of Scope

| Excluded | Why / where it lands |
|----------|----------------------|
| Conversational editing ("add this project", "change my salary") | No conversation/thread state exists anywhere. Follow-up change `edit-cv-conversational`. |
| PDF/file upload of CVs | Worker has no PDF text-extraction lib (`package.json` has only Playwright, for render). Fast-follow: add a PDF→text pre-step; pipeline shape stays the same. |
| Renaming the `scan_progress` Postgres channel | Atomic API+worker deploy risk; out of scope by design (Decision 1). |
| Retrofitting `useScanProgress.ts` for ingest events | New `useJobProgress` hook instead; scan path stays untouched to avoid regression. |

## Resolved Decisions

| # | Decision | One-line rationale |
|---|----------|--------------------|
| 1 | **WS key: scoped generalization.** Keep `scan_progress` channel; rename NOTIFY field `scan_run_id`→`run_id`; new worker route + new `useJobProgress` hook (do not touch `useScanProgress`). | Channel rename forces atomic API+worker deploy; field rename + new hook gets clean correlation with minimal blast radius (`hub.go` is already generic). |
| 2 | **Add `cv_ingestions` table** (id, user_id, status, started_at, finished_at). | Provides the `RETURNING id` correlation ID and a `GET /api/cv/ingest/:id` polling fallback if the WS drops — mirrors the proven `scan_runs` shape. |
| 3 | **One Claude call, structured parse, never-lose-the-row guard.** | Mirrors `parseEvaluationResponse`; on parse failure persist raw `cv_markdown` + `profile_json: { parse_error: true }`. Two calls double latency/cost without justification. |
| 4 | **Nested `profile_json` schema** (candidate / target_roles / salary_target / narrative). | No web consumer reads it today, so the shape is free to define; the only constraint is `pdf.mjs:25`, fixed in this change to `profile.candidate?.full_name`. |
| 5 | **Conversational editing OUT of scope.** | No conversation state in the codebase; deferred to `edit-cv-conversational` to protect the PR budget. |
| 6 | **Usage gating via `freePlanIngestLimit` + new `usage.ingestions_count` column.** | Consistent with `evaluate/service.go:24,65-77` (402 on limit); a distinct action needs a distinct counter, not `evaluations_count`. |
| 7 | **Text/markdown paste only for MVP.** | No PDF text-extraction lib in the worker; PDF upload is a noted fast-follow. |

## Impact — CLAUDE.md invariants respected

| Invariant | How this change honors it |
|-----------|---------------------------|
| Go API NEVER calls Anthropic / scrapes / launches Chromium | API only validates, inserts `cv_ingestions`, and enqueues. The `ingestCV` Claude call lives in the worker. |
| Node worker NEVER handles auth or routing | Worker only consumes the `ingest-cv` job; auth/ownership/usage gating all happen in the Go API before enqueue. |
| Every worker DB write goes through `tenantQuery(userId, …)` | The `UPDATE users SET cv_markdown…` write uses `tenantQuery`; RLS passes because `users.id` IS the tenant key (`db/rls.sql:33-35`). |
| API and worker never talk directly | Correlation flows over pg-boss (enqueue) + LISTEN/NOTIFY (`scan_progress` channel) — no direct call. |
| Strict TDD (`strict_tdd: true`) | New `worker/tests/jobs/ingest-cv.test.mjs` follows the `vi.mock` + dynamic-import shape of `evaluate.test.mjs`; Go uses `testify/mock` on the local `Servicer`; RLS verified via pgTAP for the new table. |

## Success Criteria

A user can:

- [ ] `POST /api/cv/ingest {raw_cv}` with a pasted CV → receives a `run_id` (202/200).
- [ ] See live progress over WS (`ingest.completed`) via `useJobProgress`, or poll `GET /api/cv/ingest/:id` if the socket drops.
- [ ] End state: `users.cv_markdown` is populated and `users.profile_json` holds the nested structured schema (or `{ parse_error: true }` + raw markdown on a parse miss — the row is never lost).
- [ ] A subsequent **evaluate** run reads a real profile (no longer empty), and **PDF** generation labels the candidate correctly via `profile.candidate?.full_name`.
- [ ] A free-plan user past `freePlanIngestLimit` gets a 402, consistent with evaluation gating.

## Next step

Run `sdd-spec` and `sdd-design` in parallel (both read this proposal). Spec formalizes the endpoint/event/schema contracts; design resolves the field-rename sequencing and the parse-guard format.
