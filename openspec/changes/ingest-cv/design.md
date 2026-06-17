# Design — `ingest-cv` (Conversational CV ingestion)

> Phase: design · Status: complete · Artifact store: openspec
> Inputs: `proposal.md`, `explore.md` (engram mirror `sdd/ingest-cv/explore`, obs #236)
> Skills applied: `cognitive-doc-design`, `go-testing`

## The shape in one picture

```
web (paste CV)
   │  POST /api/cv/ingest {raw_cv}
   ▼
cv.Handler.Ingest ──► cv.Service.EnqueueIngest
   │                      1. GetUserID (auth)
   │                      2. usage check vs ingestions_count  ── 402 ──►
   │                      3. INSERT cv_ingestions RETURNING id (run_id)
   │                      4. queue.Enqueue{Name:"ingest-cv", Data:{user_id,run_id,raw_cv}}
   ▼  202 {run_id}
pgboss.job ──► worker registerWorker("ingest-cv", handleIngestCV)
   │                      1. buildIngestPrompt(raw_cv)
   │                      2. ingestCV() → Claude (one call)
   │                      3. parseIngestResponse (never-throw guard)
   │                      4. tenantQuery UPDATE users SET cv_markdown, profile_json
   │                      5. tenantQuery UPDATE cv_ingestions SET status='completed'
   │                      6. tenantQuery UPSERT usage.ingestions_count +1
   │                      7. notify(client, run_id, 'ingest.completed', {...})
   ▼  pg_notify('scan_progress', {event, run_id, ...})
ws.StartListener (LISTEN scan_progress) ──► hub.Broadcast(run_id, payload)
   ▼
web useJobProgress (GET /ws/scan?token&run_id) ──► live status
        └── fallback: GET /api/cv/ingest/:id (poll status)
```

Channel name `scan_progress` is **unchanged**. The only WS rename is the JSON field `scan_run_id` → `run_id`, applied atomically in worker + Go listener (see §4).

## Decisions at a glance

| # | Decision | Rationale |
|---|----------|-----------|
| D1 | **Extend the `cv` package**, do NOT create a new `ingest` package. | The endpoints are `/api/cv/ingest*`; the domain owner is CV. Adds 2 methods to the existing `cv.Servicer`, reuses `writeJSON`/`writeError`/`ErrNotFound`. New package would duplicate all of that and split one domain across two folders. |
| D2 | **New prompt module `worker/lib/ingest-prompt.mjs`**, do NOT extend `prompt.mjs`. | `prompt.mjs` is evaluate-specific (`buildEvaluationPrompt`, 7-block contract). Mixing an ingest contract into it couples two unrelated prompts and complicates the `vi.mock('../../lib/prompt.mjs')` boundary in evaluate tests. A sibling module keeps each test's mock surface clean. |
| D3 | **Add `ingestCV()` to existing `worker/lib/anthropic.mjs`** reusing the `client` singleton. | The singleton holds the API key + timeout config (NFR-03). A second module would re-instantiate the client. Mirrors `evaluate()` exactly. |
| D4 | **WS field rename happens in ONE commit** spanning `progress.mjs` + `listener.go`, no dual-emit. | The producer (`progress.mjs`) and the only consumer (`listener.go`) are both in this repo and deploy together via docker-compose. Dual-emit (`scan_run_id` AND `run_id`) adds dead weight for a transition that never spans a release boundary here. See §4 for the atomic boundary. |
| D5 | **`run_id` is a real `cv_ingestions` row id**, not a bare UUID. | Gives `RETURNING id` correlation + a `GET /api/cv/ingest/:id` polling fallback when the socket drops (proposal Decision 2). Mirrors `scan_runs`. |
| D6 | **Parse guard never throws; on miss persist raw markdown + `{parse_error:true}`.** | Mirrors `parseEvaluationResponse` (T-58). The row is never lost; a later evaluate still has *something*. |

---

## 1. API layer — extend `api/internal/cv/`

### 1.1 Servicer interface (add 2 methods)

`api/internal/cv/handler.go` — extend the existing interface:

```go
type Servicer interface {
    // ...existing 4 methods unchanged...
    EnqueueIngest(ctx context.Context, userID uuid.UUID, rawCV string) (uuid.UUID, error)
    GetIngestion(ctx context.Context, userID, runID uuid.UUID) (*db.CvIngestion, error)
}
```

`EnqueueIngest` returns the `cv_ingestions.id` (the `run_id`) as `uuid.UUID`, not a queue-id string — the client correlates on `run_id`, not the pg-boss job id.

### 1.2 Routes (add to existing `RegisterRoutes`)

```go
r.Post("/api/cv/ingest", h.Ingest)
r.Get("/api/cv/ingest/{id}", h.GetIngestion)
```

> Ordering note: chi matches `/api/cvs` and `/api/cv/ingest` as distinct static-prefix routes — no collision. `{id}` is a path param consistent with the existing `/api/jobs/{id}/...` style.

### 1.3 Handlers (`handler.go`)

| Handler | Method | Body / param | Success | Errors |
|---------|--------|--------------|---------|--------|
| `Ingest` | POST | `{ "raw_cv": "..." }` | `202 {run_id, queued:true}` | 401 missing user; 400 empty `raw_cv` / bad JSON; 402 `ErrUsageLimitExceeded`; 500 default |
| `GetIngestion` | GET | `{id}` UUID | `200 {id,status,started_at,finished_at}` | 401; 400 bad UUID; 404 `ErrNotFound`; 500 default |

`Ingest` error mapping mirrors `evaluate/handler.go:53-62` exactly:

```go
switch {
case errors.Is(err, ErrNotFound):
    writeError(w, http.StatusNotFound, "user not found", "not_found")
case errors.Is(err, ErrUsageLimitExceeded):
    writeError(w, http.StatusPaymentRequired, err.Error(), "usage_limit_exceeded")
default:
    writeError(w, http.StatusInternalServerError, "failed to enqueue ingestion", "internal_error")
}
```

Add an empty-body guard before enqueue: `if strings.TrimSpace(body.RawCV) == "" → 400 "raw_cv required"`.

### 1.4 Service (`service.go`)

Add `ErrUsageLimitExceeded` and `freePlanIngestLimit = 5` to the `cv` package (the package currently only has `ErrNotFound`, `ErrNoPDFPath`). `EnqueueIngest` flow mirrors `evaluate/service.go:49-97`:

```go
const freePlanIngestLimit = 5

type ingestCVPayload struct {
    UserID uuid.UUID `json:"user_id"`
    RunID  uuid.UUID `json:"run_id"`
    RawCV  string    `json:"raw_cv"`
}

func (s *Service) EnqueueIngest(ctx, userID uuid.UUID, rawCV string) (uuid.UUID, error) {
    q := s.queries()
    // 1. usage check for current month
    month := time.Now().UTC().Format("2006-01")
    usage, err := q.GetUsageByUserMonth(ctx, db.GetUsageByUserMonthParams{UserID: userID, Month: month})
    if err != nil && !errors.Is(err, sql.ErrNoRows) { return uuid.Nil, fmt.Errorf("get usage: %w", err) }
    if !errors.Is(err, sql.ErrNoRows) && usage.IngestionsCount >= freePlanIngestLimit {
        return uuid.Nil, ErrUsageLimitExceeded
    }
    // 2. INSERT cv_ingestions RETURNING id
    run, err := q.InsertCVIngestion(ctx, userID)
    if err != nil { return uuid.Nil, fmt.Errorf("insert ingestion: %w", err) }
    // 3. enqueue
    payload, _ := json.Marshal(ingestCVPayload{UserID: userID, RunID: run.ID, RawCV: rawCV})
    if err := queue.Enqueue(ctx, s.pool, queue.Job{Name: "ingest-cv", Data: json.RawMessage(payload)}); err != nil {
        return uuid.Nil, fmt.Errorf("enqueue ingest-cv: %w", err)
    }
    return run.ID, nil
}
```

`GetIngestion` reads `q.GetCVIngestion(ctx, runID)`, maps `sql.ErrNoRows → ErrNotFound`. RLS makes the row invisible to other tenants, so no explicit `user_id` equality check is required (consistent with how `scan` reads `scan_runs`), but the param is kept in the signature for symmetry and future per-user logging.

> **No ownership pre-check needed** (unlike evaluate, which checks job ownership). The ingest target is the caller's own `users` row; there is no foreign object to validate. This is why `EnqueueIngest` takes `rawCV` not an id.

### 1.5 Wiring (`api/cmd/api/main.go`)

No change to the `cv.NewHandler(...)` line — the new routes register through the existing `cvHandler.RegisterRoutes(r)` call at `main.go:109`. Zero diff in `main.go`.

**Changed-line budget — API:** ~120 lines (handler ~45, service ~55, interface ~4, errors/consts ~8, imports ~8).

---

## 2. Queue — exact call

Reuses `api/internal/queue/boss.go` unchanged. The enqueue call is:

```go
queue.Enqueue(ctx, s.pool, queue.Job{
    Name: "ingest-cv",
    Data: json.RawMessage(payload), // ingestCVPayload{UserID, RunID, RawCV}
})
```

`Enqueue` already sets `state='created'`, `priority=0`, `expireIn=15m`. No queue changes. **Budget: 0 lines** (call site counted under §1.4).

---

## 3. Worker

### 3.1 `worker/jobs/ingest-cv.mjs` — `handleIngestCV(job)`

Mirrors `evaluate.mjs` structure (build → call → parse-guard → tenantQuery writes) and borrows the NOTIFY half from `scan.mjs` (acquire a `pool.connect()` client for `notify`, release in `finally`).

```js
import { tenantQuery, pool } from '../lib/db.mjs'
import { buildIngestPrompt } from '../lib/ingest-prompt.mjs'
import { ingestCV } from '../lib/anthropic.mjs'
import { notify } from '../lib/progress.mjs'

export function parseIngestResponse(responseText) {
  // never throws — returns { cvMarkdown, profileJson }
  if (!responseText || !responseText.trim()) {
    return { cvMarkdown: responseText || '', profileJson: { parse_error: true, raw: responseText } }
  }
  try {
    // Contract: two fenced sections (see §7). Split on the markers.
    const mdMatch  = responseText.match(/===CV_MARKDOWN===\s*([\s\S]*?)\s*===PROFILE_JSON===/i)
    const jsonMatch = responseText.match(/===PROFILE_JSON===\s*```json\s*([\s\S]*?)```/i)
    if (!mdMatch || !jsonMatch) {
      return { cvMarkdown: responseText, profileJson: { parse_error: true, raw: responseText } }
    }
    const cvMarkdown = mdMatch[1].trim()
    const profileJson = JSON.parse(jsonMatch[1].trim())
    return { cvMarkdown, profileJson }
  } catch {
    return { cvMarkdown: responseText, profileJson: { parse_error: true, raw: responseText } }
  }
}

export async function handleIngestCV(job) {
  const { user_id, run_id, raw_cv } = job.data
  const client = await pool.connect()
  try {
    const prompt = buildIngestPrompt(raw_cv)
    const response = await ingestCV(prompt.system, prompt.messages[0].content)
    const responseText = response.content?.[0]?.text || ''
    const { cvMarkdown, profileJson } = parseIngestResponse(responseText)
    const currentMonth = new Date().toISOString().slice(0, 7)

    // 1. write the two columns (RLS: users.id IS the tenant key)
    await tenantQuery(user_id,
      `UPDATE users SET cv_markdown = $1, profile_json = $2::jsonb WHERE id = $3::uuid`,
      [cvMarkdown, JSON.stringify(profileJson), user_id])

    // 2. mark ingestion finished
    await tenantQuery(user_id,
      `UPDATE cv_ingestions SET status = 'completed', finished_at = NOW() WHERE id = $1::uuid`,
      [run_id])

    // 3. meter usage
    await tenantQuery(user_id,
      `INSERT INTO usage (user_id, month, ingestions_count) VALUES ($1::uuid, $2, 1)
       ON CONFLICT (user_id, month) DO UPDATE SET ingestions_count = usage.ingestions_count + 1`,
      [user_id, currentMonth])

    // 4. notify (run_id field — see §4)
    await notify(client, run_id, 'ingest.completed', {
      parse_error: !!profileJson.parse_error,
    })
  } finally {
    client.release()
  }
}
```

> **`parseIngestResponse` is exported** so the vitest can table-test it directly (go-testing parser-as-pure-function principle applied to JS).

> **Failure path:** if `ingestCV` throws (network/timeout), the job throws and pg-boss retries; `cv_ingestions.status` stays `running`. The `GET /api/cv/ingest/:id` fallback shows `running`. Optionally a `try/catch` could set status `failed` + `notify('ingest.failed')` — recommended as a small addition (see §7 contract). Keep it minimal: one catch that sets `status='failed'` and emits `ingest.failed`, then rethrows for pg-boss retry telemetry.

### 3.2 `worker/lib/ingest-prompt.mjs` — `buildIngestPrompt(rawCV)`

Synchronous (no DB read — the raw CV is in the payload). Returns `{ system, messages }` shaped for `ingestCV`. One cached system block (the extraction contract), one user block (the raw CV). See §7 for the prompt contract text.

```js
export function buildIngestPrompt(rawCV) {
  return {
    system: [{ type: 'text', text: INGEST_SYSTEM_PROMPT, cache_control: { type: 'ephemeral' } }],
    messages: [{ role: 'user', content: `Here is my raw CV:\n\n${rawCV}` }],
  }
}
```

### 3.3 `worker/lib/anthropic.mjs` — add `ingestCV`

Append a second export reusing the `client` singleton (identical config to `evaluate`):

```js
export async function ingestCV(systemBlocks, userContent) {
  return client.messages.create({
    model: 'claude-sonnet-4-6',
    max_tokens: 8000,
    temperature: 0.2,
    system: systemBlocks,
    messages: [{ role: 'user', content: userContent }],
  })
}
```

### 3.4 `worker/index.mjs` — register

```js
import { handleIngestCV } from './jobs/ingest-cv.mjs'
// ...
await registerWorker('ingest-cv', handleIngestCV, { teamSize: 5 })
console.log('[worker] Registered handler: ingest-cv')
```

**Changed-line budget — Worker:** ~140 lines (ingest-cv.mjs ~75, ingest-prompt.mjs ~40, anthropic.mjs +10, index.mjs +3, progress.mjs rename §4).

---

## 4. WS generalization — the atomic boundary

This is the **only cross-service rename** in the change. It MUST land in a single commit because the producer field and the consumer struct tag are coupled.

### 4.1 What changes (exhaustive)

| File | Line(s) | Change | Risk if split |
|------|---------|--------|---------------|
| `worker/lib/progress.mjs` | 16-20 | JSON key `scan_run_id` → `run_id` | Listener can't parse → all WS (scan + ingest) silently drop |
| `api/internal/ws/listener.go` | 19 | struct tag `json:"scan_run_id"` → `json:"run_id"`; field `ScanRunID` → `RunID` | same |
| `api/internal/ws/listener.go` | 99-101 | rename local `payload.ScanRunID` usages | compile error (caught) |

### 4.2 What does NOT change (and why scan keeps working)

- **Postgres channel `scan_progress`** — unchanged everywhere (`progress.mjs:23`, `listener.go:62,65`). No deploy-coordination risk (proposal Decision 1).
- **`hub.go`** — already generic (`map[uuid.UUID]...`). Untouched.
- **`handler.go` / `/ws/scan` route / `scan_run_id` query param** — the **browser→server** query param stays `scan_run_id` for the existing scan path. The rename is purely in the **server-internal NOTIFY envelope** (`run_id`), which the browser never sees as a query param. The new ingest hook will reuse the same `/ws/scan` route but pass the ingestion `run_id` as the `scan_run_id` query param value (the param name is just a lookup key into the hub; its value is any UUID). See §4.4.
- **`scan.mjs` callers of `notify()`** — unchanged. They pass a positional `scanRunId` argument; only the JSON *key* inside `notify` changes, not the call signature.
- **`useScanProgress.ts`** — untouched (proposal: new hook instead).

> **Key insight that makes the rename safe and small:** the rename is confined to the NOTIFY payload's `event`-envelope field, which has exactly ONE producer (`progress.mjs:notify`) and ONE consumer (`listener.go:notifyPayload`). The browser-facing `scan_run_id` query param and the `scan_progress` channel name are SEPARATE strings and stay put. So the blast radius is 2 files, not the 5 the explore feared.

### 4.3 Test impact (must update in the same commit)

- `ws/handler_test.go`, `ws/hub_test.go` — assert on hub keying / handler query params, NOT on the NOTIFY JSON key. **Likely no change.** Verify by grep for `scan_run_id` in those files during apply; only update if a test asserts the envelope key literally.
- Worker scan tests — they `vi.mock('../../lib/progress.mjs')`, so the real `notify` body (where the key lives) is mocked out. **No change** unless a test asserts the serialized payload string.

> Apply-phase action: `rg 'scan_run_id' api/internal/ws worker/tests` and update only literal-key assertions. Expect 0–2 lines.

### 4.4 New web hook `web/hooks/useJobProgress.ts`

Generalized clone of `useScanProgress.ts`. Differences:

| Aspect | `useScanProgress` | `useJobProgress` |
|--------|-------------------|------------------|
| connect arg | none (implicit) | `connect(runId: string)` — passes `&scan_run_id=${runId}` |
| status enum | scan-specific | `idle｜connecting｜working｜completed｜error` |
| event match | `scan.completed` / `scan.started` | `ingest.completed` (→ completed) / `ingest.failed` (→ error) |
| URL | `/ws/scan?token=` | `/ws/scan?token=&scan_run_id=${runId}` (same route; param key unchanged server-side) |

Everything else (cleanup, reconnect-once, ref handling) copied verbatim. Scan path is never touched → zero regression by construction.

**Changed-line budget — WS+web:** ~120 lines (listener.go ~4, progress.mjs ~2, useJobProgress.ts ~110 new).

---

## 5. DB migrations

### 5.1 Migration file

New file `db/migrations/002_ingest_cv.sql` (next sequential number after `001_initial.sql`). Contains: enum (if used), table, RLS enable/force/policy, usage column.

```sql
-- 002_ingest_cv.sql

-- reuse scan_status_t? No — ingest has no 'partial'. Use a CHECK constraint instead of a new enum.
CREATE TABLE cv_ingestions (
  id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status      text        NOT NULL DEFAULT 'running'
                            CHECK (status IN ('running','completed','failed')),
  started_at  timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz
);
CREATE INDEX idx_cv_ingestions_user ON cv_ingestions(user_id, started_at DESC);

ALTER TABLE cv_ingestions ENABLE ROW LEVEL SECURITY;
ALTER TABLE cv_ingestions FORCE  ROW LEVEL SECURITY;
CREATE POLICY tenant_cv_ingestions ON cv_ingestions
  USING       (user_id = current_setting('app.current_user_id', true)::uuid)
  WITH CHECK  (user_id = current_setting('app.current_user_id', true)::uuid);

ALTER TABLE usage ADD COLUMN ingestions_count integer NOT NULL DEFAULT 0;
```

> **`text + CHECK` over a new enum.** `scan_status_t` carries `partial`/`failed` semantics ingest doesn't need; minting a 3-value enum for one table is heavier than a CHECK and harder to extend. This is a deliberate departure from the `scan_runs` enum precedent, justified by the smaller state space.

### 5.2 Mirror into the canonical files

`db/migrations/001_initial.sql` is the bootstrap; the live schema lives in `db/schema.sql` + `db/rls.sql`. Apply the SAME DDL to those two files so a fresh `schema.sql` boot matches a migrated DB:
- `db/schema.sql` — add the `cv_ingestions` table block (after `scan_runs`) + `usage.ingestions_count` column.
- `db/rls.sql` — add the three lines (ENABLE, FORCE, POLICY) for `cv_ingestions`.

### 5.3 New sqlc queries `db/queries/cv_ingestions.sql`

```sql
-- name: InsertCVIngestion :one
INSERT INTO cv_ingestions (user_id) VALUES ($1) RETURNING *;

-- name: GetCVIngestion :one
SELECT * FROM cv_ingestions WHERE id = $1 LIMIT 1;

-- name: UpdateCVIngestionStatus :one
UPDATE cv_ingestions SET status = $2, finished_at = now() WHERE id = $1 RETURNING *;
```

`db/queries/usage.sql` — add one query for the API-side increment symmetry (the worker increments inline via `tenantQuery`, but a sqlc query keeps the option open and documents the counter):

```sql
-- name: UpsertIncrementIngestions :one
INSERT INTO usage (user_id, month, ingestions_count) VALUES ($1, $2, 1)
ON CONFLICT (user_id, month) DO UPDATE
  SET ingestions_count = usage.ingestions_count + 1
RETURNING *;
```

`UpdateUserCVMarkdown` / `UpdateUserProfileJSON` already exist (`db/queries/users.sql:17-27`). The worker writes both columns in ONE `tenantQuery` UPDATE (not via sqlc — sqlc runs in the Go API; the worker uses raw `tenantQuery`). So those two queries remain available for any Go-side use but are not wired by this change. No new users.sql queries needed.

### 5.4 Regeneration step

After editing `db/queries/`: `cd db && sqlc generate`. This regenerates `api/internal/db/` including `CvIngestion` struct, `InsertCVIngestion`, `GetCVIngestion`, `UpdateCVIngestionStatus`, `GetUsageByUserMonthParams`/`Usage` (now with `IngestionsCount`). The service code in §1.4 depends on these generated symbols — generate BEFORE compiling the Go changes.

**Changed-line budget — DB:** ~70 lines (migration ~25, schema.sql ~15, rls.sql ~4, queries ~25) + generated code (not counted toward review budget; flag as generated).

---

## 6. `profile_json` schema + `pdf.mjs:25` fix

Final shape (from explore §profile_json, proposal Decision 4):

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
  "salary_target": { "min": 0, "max": 0, "currency": "string" },
  "narrative": "string"
}
```

### `pdf.mjs:25` fix (REQUIRED — in scope)

Current line reads top-level keys that the new schema does not have:

```js
const candidateName = profile.name || profile.full_name || 'Candidate'
```

Change to read the nested key, keeping the old fallbacks for safety:

```js
const candidateName = profile.candidate?.full_name || profile.name || profile.full_name || 'Candidate'
```

> Keeping the old `||` tail means any pre-existing `{}` profiles still resolve to `'Candidate'` and never crash. **Budget: 1 line.**

---

## 7. Claude prompt contract

### System block (`INGEST_SYSTEM_PROMPT`)

Instructs Claude to act as a CV parser and emit EXACTLY two delimited sections in a fixed order so a regex split is deterministic:

```
You are a CV ingestion assistant. Given a candidate's raw CV text, produce TWO sections
in this EXACT format and nothing else:

===CV_MARKDOWN===
<the CV reformatted as clean, well-structured markdown — headings, bullet lists,
preserve all factual content, do not invent>

===PROFILE_JSON===
```json
{ "candidate": { "full_name": "...", "email": "...", ... },
  "target_roles": {...}, "salary_target": {...}, "narrative": "..." }
```

Rules:
- Output the markers VERBATIM, in order: ===CV_MARKDOWN=== then ===PROFILE_JSON===.
- profile_json MUST be valid JSON inside a ```json fence.
- Use null/empty for unknown fields; never fabricate contact info or salary.
- candidate.full_name is REQUIRED if present anywhere in the CV.
```

### User block

`Here is my raw CV:\n\n${rawCV}` (the payload `raw_cv`).

### Parse guard extraction (matches §3.1 `parseIngestResponse`)

| Step | Regex | Result on miss |
|------|-------|----------------|
| markdown | `/===CV_MARKDOWN===\s*([\s\S]*?)\s*===PROFILE_JSON===/i` | guard → `{parse_error:true, raw}` |
| json | `/===PROFILE_JSON===\s*\`\`\`json\s*([\s\S]*?)\`\`\`/i` then `JSON.parse` | guard → `{parse_error:true, raw}`, but raw markdown still persisted |

On ANY miss (no markers, malformed JSON), persist the WHOLE `responseText` as `cv_markdown` and `profile_json = {parse_error:true, raw}`. The row is never lost (D6). `notify('ingest.completed', {parse_error:true})` so the UI can warn.

---

## 8. Test strategy (TDD — tests first)

go-testing skill applied: parsers get table-driven tests; error behavior gets explicit success + failure cases; mock at the system boundary (Servicer, tenantQuery, anthropic).

| Layer | File | Pattern | Scenarios |
|-------|------|---------|-----------|
| Go handler | `api/internal/cv/handler_test.go` (extend) | `testify/mock` on `Servicer` | Ingest: 202 happy; 400 empty raw_cv; 402 limit; 401 no user. GetIngestion: 200; 404; 400 bad UUID |
| Go service | `api/internal/cv/service_test.go` (extend) | real pool OR mock queries per existing style | usage under limit → enqueue + run_id; usage at limit → `ErrUsageLimitExceeded`; no usage row → enqueue (count 0) |
| Worker job | `worker/tests/jobs/ingest-cv.test.mjs` (new) | `vi.mock` db/ingest-prompt/anthropic/progress + dynamic import (mirror `evaluate.test.mjs:1-19`) | happy: valid 2-section → UPDATE users + cv_ingestions + usage + notify; parse miss → raw persisted, `{parse_error:true}`, status completed, notify with parse_error; anthropic throws → status failed path |
| Worker parser | same file or `worker/tests/lib/ingest-prompt.test.mjs` | table-driven over `parseIngestResponse` | valid; missing markers; malformed JSON; empty string; markdown-only |
| pgTAP RLS | `db/tests/cv_ingestions_rls.test.sql` (new, mirror existing RLS tests) | set `app.current_user_id`, assert cross-tenant invisibility | user A inserts ingestion → user B cannot SELECT/UPDATE; FORCE RLS active |
| Web hook | `web/__tests__/hooks/useJobProgress.test.tsx` (new) | vitest + mock WebSocket (mirror scan hook tests) | connect sets connecting→working; `ingest.completed` → completed; `ingest.failed` → error; reconnect-once |

**TDD order per slice:** (1) write failing test, (2) implement, (3) green, (4) run narrow package then broader suite (`make test-go`, `cd worker && npm test`, `make test-rls`, `make test-web`).

**Commands:**
```
cd api && go test ./internal/cv/... -count=1 -v
cd worker && npx vitest run tests/jobs/ingest-cv.test.mjs
make test-rls
cd web && npx vitest run __tests__/hooks/useJobProgress.test.tsx
```

---

## 9. Sequencing & risks

### Recommended apply order (dependency-respecting)

1. **DB first** — migration + schema.sql + rls.sql + queries → `sqlc generate`. (Go code won't compile without generated `CvIngestion`/`IngestionsCount`.)
2. **Go API** — service + handler + tests. Depends on step 1's generated code.
3. **Worker** — ingest-cv.mjs, ingest-prompt.mjs, anthropic.mjs, index.mjs + tests. Independent of Go compile.
4. **WS rename (atomic)** — progress.mjs + listener.go in ONE commit, plus any literal-key test fixes. Do this as its own commit so the 2-file coupling is auditable.
5. **Web hook + UI** — useJobProgress.ts + paste-CV page + tests. Depends on the WS field name from step 4.
6. **pdf.mjs:25 fix** — 1 line, can ride with step 3 or stand alone.

### Risks

| Risk | Severity | Mitigation |
|------|----------|------------|
| WS field rename breaks scan WS if listener/producer drift | High | D4: single commit, no dual-emit; grep test fixtures in same commit (§4.3) |
| sqlc not regenerated before Go compile | Medium | §5.4 explicit order; CI `go build` catches it |
| `cv_ingestions.status` enum vs CHECK divergence between schema.sql and migration | Medium | §5.2 mirror DDL verbatim in both files; pgTAP RLS test boots from schema.sql |
| Claude returns markers inconsistently → frequent parse_error | Medium | D6 guard never loses row; system prompt demands VERBATIM markers; parser table-test covers misses |
| Browser query param still named `scan_run_id` while envelope is `run_id` — confusing | Low | §4.2: intentional; param is a hub lookup key, value is the ingestion run_id; documented in hook |
| `usage.ingestions_count` default backfill on existing rows | Low | `NOT NULL DEFAULT 0` backfills existing rows automatically |

### Estimated total changed lines (feeds Review Workload Forecast)

| Area | Lines |
|------|-------|
| DB (hand-written) | ~70 |
| Go API | ~120 |
| Worker | ~140 |
| WS + web hook | ~120 |
| pdf fix | ~1 |
| Tests (Go + worker + pgTAP + web) | ~250 |
| **Total (hand-written, excl. generated sqlc)** | **~700** |

> **Over the 400-line budget.** Recommend the tasks phase plan chained/stacked slices along the §9 apply order — natural seams: (A) DB+sqlc, (B) Go API+tests, (C) worker+tests+pdf, (D) WS rename atomic, (E) web hook+UI. Slice D must stay a single atomic commit regardless of how the PRs are grouped.

## Next step

Run `sdd-tasks` (reads `spec.md` + this `design.md`). Tasks should establish the next `T-NN` ID range and produce the Review Workload Forecast flagging the ~700-line budget → chained PRs along the A–E seams above.
