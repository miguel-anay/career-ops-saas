# Spec — `ingest-cv` (Conversational CV ingestion)

> Phase: spec · Status: complete · Artifact store: openspec
> Input: `openspec/changes/ingest-cv/proposal.md` (decisions 1-7), `openspec/changes/ingest-cv/explore.md`

This is a delta spec: it defines what MUST be true after `ingest-cv` ships. Every scenario below is written so a test can be authored against it BEFORE the implementation exists (strict TDD).

## Requirement 1 — `POST /api/cv/ingest` enqueues an ingestion run

The API MUST accept a raw CV from an authenticated user, gate on usage limits, persist a `cv_ingestions` row, and enqueue a worker job — without ever calling Anthropic itself.

#### Scenario: Authenticated user submits a valid CV
- **Given** a JWT-authenticated user with `usage.ingestions_count < freePlanIngestLimit` for the current month
- **When** they `POST /api/cv/ingest` with body `{"raw_cv": "<non-empty text>"}`
- **Then** the API responds `202 Accepted` with JSON `{"run_id": "<uuid>"}`
- **And** a `cv_ingestions` row exists with `user_id = caller`, `status = 'pending'`, `started_at` set, `finished_at` null
- **And** a pg-boss job named `ingest-cv` is enqueued with payload `{"user_id": "<uuid>", "run_id": "<uuid>", "raw_cv": "<text>"}`
- **And** the Go API process makes zero calls to Anthropic (handler/service code path contains no Anthropic client usage)

#### Scenario: Missing or invalid JWT
- **Given** a request with no `Authorization` header, or an invalid/expired JWT
- **When** the caller `POST`s `/api/cv/ingest`
- **Then** the API responds `401 Unauthorized` with `{"error": "...", "code": "unauthorized"}`
- **And** no `cv_ingestions` row is created and no job is enqueued

#### Scenario: Empty body rejected
- **Given** an authenticated user
- **When** they `POST /api/cv/ingest` with `{"raw_cv": ""}` or `{"raw_cv": "   "}` (whitespace-only) or a missing `raw_cv` field
- **Then** the API responds `400 Bad Request` with `{"error": "...", "code": "invalid_body"}`
- **And** no `cv_ingestions` row is created and no job is enqueued

#### Scenario: Oversized body rejected
- **Given** an authenticated user
- **When** they `POST /api/cv/ingest` with a `raw_cv` value exceeding the configured maximum length
- **Then** the API responds `400 Bad Request` with `{"error": "...", "code": "invalid_body"}`
- **And** no `cv_ingestions` row is created and no job is enqueued

#### Scenario: Usage limit exceeded (free plan)
- **Given** an authenticated free-plan user whose `usage.ingestions_count` for the current month is `>= freePlanIngestLimit`
- **When** they `POST /api/cv/ingest` with a valid body
- **Then** the API responds `402 Payment Required` with `{"error": "...", "code": "usage_limit_exceeded"}`
- **And** no `cv_ingestions` row is created and no job is enqueued

#### Scenario: First ingestion of the month has no usage row yet
- **Given** an authenticated user with no `usage` row for the current month
- **When** they `POST /api/cv/ingest` with a valid body
- **Then** the request succeeds as in the "valid CV" scenario (missing usage row is treated as `ingestions_count = 0`, mirroring `evaluate/service.go:70-77`)

## Requirement 2 — `GET /api/cv/ingest/:id` returns ingestion status with tenant isolation

The status endpoint MUST let a caller poll the state of their own ingestion run, and MUST be invisible to every other tenant — RLS-backed, not just app-layer filtering.

#### Scenario: Owner polls their own ingestion
- **Given** a `cv_ingestions` row with `id = X`, `user_id = A`
- **When** user `A` calls `GET /api/cv/ingest/X`
- **Then** the API responds `200 OK` with `{"id": "X", "status": "<pending|processing|completed|failed>", "started_at": "...", "finished_at": "..." | null}`

#### Scenario: Non-owner cannot see another tenant's ingestion
- **Given** a `cv_ingestions` row with `id = X`, `user_id = A`
- **When** user `B` (a different authenticated user) calls `GET /api/cv/ingest/X`
- **Then** the API responds `404 Not Found` with `{"error": "...", "code": "not_found"}`
- **And** the row is never serialized into the response (RLS denies the row at the query layer, not a manual ownership `if`)

#### Scenario: Ingestion id does not exist
- **Given** no `cv_ingestions` row with `id = Y` exists
- **When** an authenticated user calls `GET /api/cv/ingest/Y`
- **Then** the API responds `404 Not Found` with `{"error": "...", "code": "not_found"}`

#### Scenario: Malformed id
- **Given** an authenticated user
- **When** they call `GET /api/cv/ingest/not-a-uuid`
- **Then** the API responds `400 Bad Request` with `{"error": "...", "code": "invalid_id"}`

#### Scenario: Unauthenticated status check
- **Given** a request with no valid JWT
- **When** the caller calls `GET /api/cv/ingest/X` for any `X`
- **Then** the API responds `401 Unauthorized`

## Requirement 3 — Worker `ingest-cv` job processes the CV without losing the row

The worker MUST call Claude exactly once per job, parse the response for both `cv_markdown` and `profile_json` using a guard that never throws, and always leave the ingestion row in a terminal, inspectable state.

#### Scenario: Successful parse — happy path
- **Given** a pg-boss `ingest-cv` job with payload `{user_id, run_id, raw_cv}`
- **When** `handleIngestCV(job)` runs and Claude returns a response containing both a CV markdown block and a valid JSON `profile_json` block
- **Then** exactly one call is made to the Anthropic client (`ingestCV(...)` in `worker/lib/anthropic.mjs`)
- **And** `tenantQuery(userId, 'UPDATE users SET cv_markdown=$1, profile_json=$2 WHERE id=$3', [cv_markdown, profile_json, userId])` is executed
- **And** the `cv_ingestions` row for `run_id` is updated to `status = 'completed'`, `finished_at` set
- **And** `notify(client, run_id, 'ingest.completed', { ... })` is called on the `scan_progress` channel

#### Scenario: Claude response fails to parse — row is never lost
- **Given** a pg-boss `ingest-cv` job with payload `{user_id, run_id, raw_cv}`
- **When** Claude's response cannot be parsed into a valid `cv_markdown` + `profile_json` pair (malformed delimiters, invalid JSON, etc.)
- **Then** the parse guard does NOT throw and does NOT crash the job handler
- **And** `users.cv_markdown` is still written with the raw text extracted/returned by Claude (best-effort, never null on this path)
- **And** `users.profile_json` is written as `{"parse_error": true}`
- **And** the `cv_ingestions` row for `run_id` is updated to `status = 'failed'`, `finished_at` set (the row is preserved, never deleted)
- **And** `notify(client, run_id, 'ingest.failed', { ... })` is called

#### Scenario: Anthropic call itself throws (network/API error)
- **Given** a pg-boss `ingest-cv` job
- **When** the call to `ingestCV(...)` throws (timeout, 5xx, rate limit)
- **Then** the `cv_ingestions` row is updated to `status = 'failed'`, `finished_at` set
- **And** `notify(client, run_id, 'ingest.failed', { ... })` is called
- **And** the job handler does not leave the row stuck in `pending`/`processing` indefinitely

#### Scenario: Worker write respects tenant isolation
- **Given** a job payload with `user_id = A`
- **When** the worker performs the `UPDATE users SET cv_markdown=..., profile_json=...` write
- **Then** the write MUST go through `tenantQuery(userId, ...)` (sets `app.current_user_id` via `SET LOCAL` inside the transaction)
- **And** a direct, non-`tenantQuery` write path is absent from the job handler (no raw pool query bypassing RLS)

#### Scenario: Job processing transitions status before completion
- **Given** a `cv_ingestions` row created at enqueue time with `status = 'pending'`
- **When** `handleIngestCV(job)` begins processing
- **Then** the row transitions to `status = 'processing'` before the Claude call completes (so `GET /api/cv/ingest/:id` polling reflects in-flight work, not just pending/terminal states)

## Requirement 4 — `profile_json` schema is nested and `pdf.mjs` reads the new shape

The worker MUST persist `profile_json` using the nested schema below, and `worker/jobs/pdf.mjs` MUST read the candidate's name from the new nested path (not the old top-level key).

#### Scenario: profile_json conforms to the nested schema
- **Given** a successful parse of a Claude ingestion response
- **When** `profile_json` is persisted
- **Then** it matches this shape (fields may be empty strings/arrays but the keys MUST be present):
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

#### Scenario: PDF generation reads the nested candidate name
- **Given** a `users` row with `profile_json = {"candidate": {"full_name": "Jane Doe", ...}, ...}`
- **When** `worker/jobs/pdf.mjs` builds the CV PDF HTML
- **Then** the candidate name label is read via `profile.candidate?.full_name` (not `profile.name || profile.full_name`)
- **And** it renders `"Jane Doe"` as the label

#### Scenario: PDF generation tolerates a parse-error profile
- **Given** a `users` row with `profile_json = {"parse_error": true}` (the never-lose-the-row fallback from Requirement 3)
- **When** `worker/jobs/pdf.mjs` builds the CV PDF HTML
- **Then** `profile.candidate?.full_name` evaluates to `undefined` without throwing
- **And** the PDF generation proceeds with a fallback/empty label rather than crashing the job

## Requirement 5 — WebSocket progress delivery by `run_id`

The browser MUST be able to subscribe to live progress for an ingestion run and receive a terminal `ingest.completed` or `ingest.failed` event keyed by `run_id`.

#### Scenario: Browser receives ingest.completed over WS
- **Given** a browser connected to the WS hub and subscribed with a known `run_id` (returned from `POST /api/cv/ingest`)
- **When** the worker calls `notify(client, run_id, 'ingest.completed', {...})` on the `scan_progress` Postgres channel
- **Then** `api/internal/ws/listener.go` receives the NOTIFY payload with JSON field `run_id` (not `scan_run_id`)
- **And** `hub.Broadcast` delivers the event to the connection keyed by that `run_id`
- **And** the browser's `useJobProgress` hook surfaces `ingest.completed` with the structured payload (no other connected `run_id` receives this event)

#### Scenario: Two ingestion runs on the WS hub do not cross-deliver events
- **Given** two browser connections subscribed to `run_id = X` and `run_id = Y` respectively (different users or same user, different runs)
- **When** the worker emits `notify(client, X, 'ingest.completed', {...})`
- **Then** only the connection keyed by `X` receives the message
- **And** the connection keyed by `Y` receives nothing for this event

#### Scenario: Browser receives ingest.failed over WS
- **Given** a browser subscribed with a known `run_id`
- **When** the worker calls `notify(client, run_id, 'ingest.failed', {...})` (parse error or Anthropic error path)
- **Then** `useJobProgress` surfaces `ingest.failed` with whatever diagnostic payload was sent
- **And** the hook transitions out of any "in progress" UI state

#### Scenario: WS drops — status endpoint is the fallback
- **Given** a browser whose WS connection drops after submitting an ingestion
- **When** the browser instead polls `GET /api/cv/ingest/:run_id`
- **Then** it receives the same terminal state (`completed`/`failed`) that the WS event would have carried, per Requirement 2

#### Scenario: scan_progress channel is unchanged, only the field is renamed
- **Given** the existing `scan.mjs` → `progress.mjs` → `listener.go` → `useScanProgress.ts` path (scan domain)
- **When** `ingest-cv` reuses `progress.mjs`'s `notify()` helper with the field renamed `scan_run_id` → `run_id`
- **Then** existing scan-domain WS tests and behavior are unaffected (no LISTEN channel rename, no change to `useScanProgress.ts`)

## Requirement 6 — Database invariants: RLS and usage accounting

`cv_ingestions` MUST be tenant-isolated at the database layer, and ingestion usage MUST be tracked per user per month for gating.

#### Scenario: cv_ingestions has forced RLS
- **Given** the `cv_ingestions` table migration
- **When** the schema is inspected
- **Then** `cv_ingestions` has `FORCE ROW LEVEL SECURITY` enabled
- **And** a tenant policy exists scoped to `user_id = current_setting('app.current_user_id', true)::uuid`, mirroring `db/rls.sql` conventions for other tenant tables

#### Scenario: pgTAP verifies cross-tenant row invisibility
- **Given** two `cv_ingestions` rows owned by different users, `A` and `B`
- **When** a pgTAP test sets `app.current_user_id` to `A`'s id and selects from `cv_ingestions`
- **Then** only `A`'s row is visible; `B`'s row is absent from the result set

#### Scenario: ingestions_count increments on enqueue
- **Given** a user with no `usage` row for the current month
- **When** `POST /api/cv/ingest` succeeds (Requirement 1, happy path)
- **Then** a `usage` row for `(user_id, current_month)` exists with `ingestions_count = 1` (UPSERT semantics — insert if absent, increment if present)

#### Scenario: ingestions_count increments independently of evaluations_count
- **Given** a user with an existing `usage` row for the current month where `evaluations_count = 3`
- **When** the user successfully submits two `POST /api/cv/ingest` calls within the usage limit
- **Then** `usage.ingestions_count = 2` for that month
- **And** `usage.evaluations_count` remains `3` (unaffected — distinct counters per Decision 6)

#### Scenario: Failed/rejected requests do not increment usage
- **Given** a user at or over `freePlanIngestLimit`
- **When** their `POST /api/cv/ingest` is rejected with `402`
- **Then** `usage.ingestions_count` is NOT incremented by the rejected request

## Cross-cutting: tenant isolation summary

Every scenario touching `cv_ingestions`, `users.cv_markdown`, `users.profile_json`, or `usage.ingestions_count` MUST be verified to deny cross-tenant access at the RLS layer, not merely via an application-level ownership check — consistent with ADR-3 (RLS over app-layer filtering). A user must never be able to read, infer the existence of, or poll the status of another user's ingestion run.
