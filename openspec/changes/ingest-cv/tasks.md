# Tasks — `ingest-cv` (Conversational CV ingestion)

> Phase: tasks · Status: complete · Artifact store: openspec
> Input: `openspec/changes/ingest-cv/spec.md`, `openspec/changes/ingest-cv/design.md`, `openspec/changes/ingest-cv/proposal.md`
> Task ID range: **T-79 .. T-104** (contiguous, fresh range — highest existing ID found in repo is `T-78` in `docker-compose.yml`/`integration_test.go`)
> Strict TDD is active. Every implementation task is preceded by its test task. Order within each seam is test-first.

## Seam map (PR boundaries)

| Seam | Scope | Spec requirements covered |
|------|-------|----------------------------|
| **A** | DB schema + migration + sqlc | Req 6 (RLS, usage accounting) — foundation for Req 1, 2, 3 |
| **B** | Go API ingest/status endpoints | Req 1 (`POST /api/cv/ingest`), Req 2 (`GET /api/cv/ingest/:id`) |
| **C** | Worker `ingest-cv` job + `pdf.mjs` fix | Req 3 (job processing), Req 4 (`profile_json` schema + pdf fix) |
| **D** | WS field rename `scan_run_id`→`run_id` (ATOMIC) | Req 5 (WS progress delivery) — enabling change, no new behavior |
| **E** | Web hook `useJobProgress` + UI | Req 5 (browser delivery), Req 1/2 (UI wiring to endpoints) |

Dependency edges: **A → B**, **A → C** (worker needs `cv_ingestions` row + columns), **D is independent of A/B/C** (pure rename, can land anytime before C's NOTIFY ships to production) but **C's NOTIFY call must consume the post-rename field**, so the sequencing below pins **D before C** lands its NOTIFY call. **E depends on D** (hook reads `run_id`) **and on B** (hook calls the new endpoints).

---

## Seam A — DB schema + migration + sqlc

**Spec coverage:** Requirement 6 (RLS forced on `cv_ingestions`, usage accounting columns). Foundation dependency for Req 1, 2, 3 (Go/worker code references generated `CvIngestion` struct and `IngestionsCount` field).

| ID | Type | Description | File(s) |
|----|------|--------------|---------|
| T-79 | test | Write pgTAP RLS test asserting `cv_ingestions` forces RLS and a tenant policy denies cross-tenant SELECT (two users, A's row invisible to B) — Req 6 scenarios "forced RLS" + "pgTAP verifies cross-tenant row invisibility" | `db/tests/cv_ingestions_rls.test.sql` |
| T-80 | impl | Add `cv_ingestions` table (id, user_id, status CHECK, started_at, finished_at), index, `ENABLE`/`FORCE ROW LEVEL SECURITY`, tenant policy; add `usage.ingestions_count INT NOT NULL DEFAULT 0` — makes T-79 green | `db/migrations/002_ingest_cv.sql` |
| T-81 | impl | Mirror the same DDL into the canonical bootstrap files so a fresh boot matches a migrated DB | `db/schema.sql`, `db/rls.sql` |
| T-82 | impl | Add sqlc queries: `InsertCVIngestion`, `GetCVIngestion`, `UpdateCVIngestionStatus`, `UpsertIncrementIngestions` | `db/queries/cv_ingestions.sql`, `db/queries/usage.sql` |
| T-83 | impl | Regenerate sqlc Go types (`CvIngestion` struct, query methods, `Usage.IngestionsCount`) — required before any Go API code in Seam B compiles | `db/sqlc.yaml` (no edit, just run) → generates `api/internal/db/*.go` |
| T-84 | verify | Run `make test-rls` to confirm T-79 passes against the live migration | n/a (verification step) |

**Sequencing within A:** T-79 → T-80 → T-81 → T-82 → T-83 → T-84. Strictly sequential (each step depends on the prior file existing).

---

## Seam B — Go API ingest/status endpoints

**Spec coverage:** Requirement 1 (`POST /api/cv/ingest` — all 5 scenarios), Requirement 2 (`GET /api/cv/ingest/:id` — all 5 scenarios), Requirement 6 (usage increment scenarios, enforced via the service layer calling generated sqlc queries from Seam A).

**Depends on:** Seam A (T-83 generated types).

| ID | Type | Description | File(s) |
|----|------|--------------|---------|
| T-85 | test | Extend `cv` service tests: usage under limit → enqueue + returns `run_id`; usage at/over limit → `ErrUsageLimitExceeded`; no usage row for month → treated as count 0 (Req 1 scenarios: valid CV, usage limit exceeded, first ingestion of month) | `api/internal/cv/service_test.go` |
| T-86 | impl | Add `ErrUsageLimitExceeded`, `freePlanIngestLimit = 5`, `EnqueueIngest(ctx, userID, rawCV)` to `cv.Service` — usage check → `InsertCVIngestion` → `queue.Enqueue("ingest-cv", {user_id,run_id,raw_cv})` — makes T-85 green | `api/internal/cv/service.go` |
| T-87 | test | Extend `cv` service tests for `GetIngestion`: owner reads own row → maps to response shape; `sql.ErrNoRows` → `ErrNotFound` (Req 2 scenarios: owner polls, ingestion id does not exist) | `api/internal/cv/service_test.go` |
| T-88 | impl | Add `GetIngestion(ctx, userID, runID) (*db.CvIngestion, error)` to `cv.Service` — calls `GetCVIngestion`, maps `sql.ErrNoRows`→`ErrNotFound` — makes T-87 green | `api/internal/cv/service.go` |
| T-89 | test | Extend `cv` handler tests with `testify/mock` on `Servicer`: `Ingest` → 202 happy path, 400 empty/whitespace `raw_cv`, 400 oversized body, 402 usage limit, 401 missing user (Req 1 all scenarios) | `api/internal/cv/handler_test.go` |
| T-90 | impl | Add `Ingest` handler: parse body, empty/whitespace guard, max-length guard, call `EnqueueIngest`, error mapping mirroring `evaluate/handler.go` (`ErrUsageLimitExceeded`→402, default→500) — makes T-89 green | `api/internal/cv/handler.go` |
| T-91 | test | Extend `cv` handler tests for `GetIngestion`: 200 with status shape, 404 not found, 404 non-owner (RLS-backed, not app-layer check), 400 malformed UUID, 401 unauthenticated (Req 2 all scenarios) | `api/internal/cv/handler_test.go` |
| T-92 | impl | Add `GetIngestion` handler + 2 routes (`POST /api/cv/ingest`, `GET /api/cv/ingest/{id}`) registered in existing `RegisterRoutes` — makes T-91 green | `api/internal/cv/handler.go` |
| T-93 | test | Add `Servicer` interface methods (`EnqueueIngest`, `GetIngestion`) and regenerate/hand-write the mock used by T-89/T-91 | `api/internal/cv/handler.go` (interface), mock file per existing convention |
| T-94 | verify | Run `make test-go` (or `cd api && go test ./internal/cv/... -count=1 -v`) to confirm the full `cv` package is green end to end | n/a (verification step) |

**Sequencing within B:** T-85→T-86 (service/enqueue), T-87→T-88 (service/status) can run in parallel with T-85/T-86 since they touch different methods on the same file but are logically independent — sequential commits recommended to keep diffs small: T-85→T-86→T-87→T-88→T-89→T-93→T-90→T-91→T-92→T-94. T-93 (interface + mock) must land before or with T-89 since the handler test mocks the interface — listed adjacent to make that explicit.

---

## Seam C — Worker `ingest-cv` job + `pdf.mjs` fix

**Spec coverage:** Requirement 3 (all 5 scenarios — happy path, parse failure never loses the row, Anthropic throw, tenant isolation, status transitions), Requirement 4 (both scenarios — nested `profile_json` schema, `pdf.mjs` reads nested path + tolerates parse-error profile).

**Depends on:** Seam A (the job writes to `cv_ingestions`, needs the table to exist for integration-style tests; the table is config, not a Go compile dependency, so this seam can be developed in parallel with Seam B but must run against a DB that has Seam A's migration applied). **Depends on Seam D landing first** for the NOTIFY field name the job emits (see Seam D below) — sequencing note in Seam D section.

| ID | Type | Description | File(s) |
|----|------|--------------|---------|
| T-95 | test | Table-driven test for `parseIngestResponse`: valid 2-section response, missing markers, malformed JSON, empty string, markdown-only — never throws in any case (Req 3 scenario: Claude response fails to parse) | `worker/tests/jobs/ingest-cv.test.mjs` (or `worker/tests/lib/ingest-prompt.test.mjs` per design §8) |
| T-96 | impl | Implement `parseIngestResponse(responseText)` — regex split on `===CV_MARKDOWN===`/`===PROFILE_JSON===` markers, `JSON.parse` guard, returns `{parse_error:true, raw}` on any miss — makes T-95 green | `worker/jobs/ingest-cv.mjs` |
| T-97 | test | Add `buildIngestPrompt(rawCV)` unit test — returns `{system, messages}` shape with cached system block + user content containing raw CV | `worker/tests/lib/ingest-prompt.test.mjs` |
| T-98 | impl | Implement `buildIngestPrompt` + `INGEST_SYSTEM_PROMPT` contract text (design §7) — makes T-97 green | `worker/lib/ingest-prompt.mjs` |
| T-99 | test | Add `ingestCV()` test mirroring existing `evaluate()` anthropic test — asserts client singleton called with `claude-sonnet-4-6`, `max_tokens:8000`, `temperature:0.2`, correct system/messages | `worker/tests/lib/anthropic.test.mjs` (extend) |
| T-100 | impl | Add `ingestCV(systemBlocks, userContent)` export to `worker/lib/anthropic.mjs` reusing the `client` singleton — makes T-99 green | `worker/lib/anthropic.mjs` |
| T-101 | test | Write `handleIngestCV(job)` integration-style test with `vi.mock` on db/ingest-prompt/anthropic/progress (mirror `evaluate.test.mjs` dynamic-import shape): happy path (one Anthropic call, `tenantQuery` UPDATE users + cv_ingestions completed + usage upsert + `notify('ingest.completed')`); parse-miss path (raw persisted, `{parse_error:true}`, status completed, notify carries `parse_error:true`); Anthropic-throws path (status failed, `notify('ingest.failed')`, row never stuck pending); tenant-isolation assertion (write goes through `tenantQuery`, no raw pool query) (Req 3 — all 5 scenarios) | `worker/tests/jobs/ingest-cv.test.mjs` |
| T-102 | impl | Implement `handleIngestCV(job)`: transition row to `processing` before the Claude call, build prompt → `ingestCV()` (exactly once) → `parseIngestResponse` → `tenantQuery` UPDATE users (cv_markdown, profile_json) → `tenantQuery` UPDATE cv_ingestions (status, finished_at) → `tenantQuery` UPSERT usage.ingestions_count → `notify(client, run_id, 'ingest.completed'/'ingest.failed', {...})`; wrap in try/catch so an Anthropic throw still marks the row `failed` and notifies, never leaving it stuck — makes T-101 green | `worker/jobs/ingest-cv.mjs` |
| T-103 | test | Register the job in worker bootstrap test (or smoke-check) confirming `ingest-cv` is wired with `registerWorker('ingest-cv', handleIngestCV, {teamSize:5})` | `worker/index.mjs` test coverage (extend existing worker bootstrap test if present, else inline assertion) |
| T-104 | impl | Register `ingest-cv` handler in worker bootstrap — makes T-103 green | `worker/index.mjs` |
| T-105 | test | Write/extend `pdf.mjs` test asserting candidate name reads `profile.candidate?.full_name` for the new nested schema AND tolerates `{parse_error:true}` profile without throwing, falling back to `'Candidate'` (Req 4 — both pdf scenarios) | `worker/tests/jobs/pdf.test.mjs` (extend) |
| T-106 | impl | Fix `pdf.mjs:25` — `profile.candidate?.full_name \|\| profile.name \|\| profile.full_name \|\| 'Candidate'` — makes T-105 green | `worker/jobs/pdf.mjs` |
| T-107 | verify | Run `cd worker && npm test` to confirm the full worker suite (existing scan/evaluate tests + new ingest-cv/pdf tests) is green | n/a (verification step) |

**Sequencing within C:** T-95→T-96 → T-97→T-98 → T-99→T-100 → T-101→T-102 → T-103→T-104 → T-105→T-106 → T-107. The parser/prompt/anthropic unit tests (T-95–T-100) can be written and implemented in parallel with each other (independent files/functions) but are listed sequentially for a clean commit story; T-101/T-102 (the integration test) depends on all three being done since `handleIngestCV` imports all of them. T-105/T-106 (pdf fix) is independent of the rest of C and could be its own micro-commit, but is grouped into Seam C per the design's "1 line, can ride with step 3" note.

> **Note on task ID T-102's NOTIFY call:** `handleIngestCV` calls `notify(client, run_id, 'ingest.completed', {...})` using the **post-rename** field name (`run_id`, not `scan_run_id`). This requires Seam D's rename to have landed in `worker/lib/progress.mjs` BEFORE T-102 is implemented (see Seam D sequencing below) — otherwise T-102 would need a temporary `scan_run_id` reference that gets immediately revised, producing a noisy diff. **Chosen sequencing: Seam D lands first, then Seam C's T-102 is written directly against the renamed field — no temporary/dual-emit code.**

---

## Seam D — WS field rename `scan_run_id` → `run_id` (ATOMIC)

**Spec coverage:** Requirement 5, scenario "scan_progress channel is unchanged, only the field is renamed" — this seam is the enabling rename itself; it has no independent user-facing scenario but is the precondition for Req 5's "Browser receives ingest.completed/failed over WS" and "Two ingestion runs do not cross-deliver" scenarios, which Seam E's tests will exercise end-to-end.

**This seam MUST be ONE commit.** No dual-emit, no transition period — the producer (`progress.mjs`) and the only consumer (`listener.go`) deploy together via docker-compose, per design Decision D4.

**Depends on:** nothing structurally (it's a pure rename of an existing field used only by the scan domain today). **Must land before Seam C's T-102** per the sequencing note above, and **before Seam E** (the new hook is written directly against `run_id`).

| ID | Type | Description | File(s) |
|----|------|--------------|---------|
| T-108 | test | Before the rename: `rg 'scan_run_id' api/internal/ws worker/tests` to inventory every literal-key assertion that needs updating in the same commit (per design §4.3) — this is a discovery/audit task, not a new test file, but its output gates which existing test files get touched in T-109 | n/a (audit — informs T-109 file list) |
| T-109 | impl + test (single atomic commit) | Rename `notifyPayload.ScanRunID` (`json:"scan_run_id"`) → `RunID` (`json:"run_id"`) in the Go listener; rename the JSON key in `progress.mjs`'s `notify()` helper; update any literal-key assertions found in T-108 in the SAME commit. Postgres channel name `scan_progress` and the browser-facing `scan_run_id` query param are explicitly UNCHANGED. | `worker/lib/progress.mjs`, `api/internal/ws/listener.go`, plus any literal-key test fixtures found by T-108 (e.g. `ws/handler_test.go`, `ws/hub_test.go` only if they assert the envelope key, not the query param) |
| T-110 | verify | Run `make test-go` (ws package) and `cd worker && npm test` (scan tests) to confirm zero regression in the existing scan WS path — this is the safety net proving the rename didn't break scan | n/a (verification step) |

**Sequencing within D:** T-108 → T-109 → T-110, strictly sequential, all inside the boundary of one commit for T-109 (T-108 is read-only audit, T-110 is post-commit verification — both can be separate commits/steps, only T-109 itself must be atomic).

---

## Seam E — Web hook `useJobProgress` + UI

**Spec coverage:** Requirement 5 (all scenarios — `ingest.completed`/`ingest.failed` delivery, no cross-run delivery, WS-drop fallback to status polling), Requirement 1 & 2 (UI wiring — paste-CV submission calling `POST /api/cv/ingest`, status display calling `GET /api/cv/ingest/:id`).

**Depends on:** Seam D (the hook is written directly against the renamed `run_id` field) and Seam B (the hook/UI calls the new endpoints).

| ID | Type | Description | File(s) |
|----|------|--------------|---------|
| T-111 | test | Write `useJobProgress` hook tests with mock WebSocket (mirror `useScanProgress` test shape): `connect(runId)` transitions idle→connecting→working; `ingest.completed` event → status `completed` with payload surfaced; `ingest.failed` event → status `error`; reconnect-once behavior; two different `run_id`s do not cross-deliver (Req 5 — all scenarios) | `web/__tests__/hooks/useJobProgress.test.tsx` |
| T-112 | impl | Implement `useJobProgress(runId)` hook — generalized clone of `useScanProgress.ts` per design §4.4 table (status enum `idle\|connecting\|working\|completed\|error`, connects to `/ws/scan?token=&scan_run_id=${runId}` reusing the existing route/param name, matches on `ingest.completed`/`ingest.failed` events) — makes T-111 green | `web/hooks/useJobProgress.ts` |
| T-113 | test | Write/extend web component test for the paste-CV page: submit calls `POST /api/cv/ingest`, receives `run_id`, renders live status via `useJobProgress`, falls back to polling `GET /api/cv/ingest/:id` on WS drop | `web/__tests__/app/ingest-cv.test.tsx` (or appropriate route test file) |
| T-114 | impl | Build the paste-CV UI: textarea + submit button + live status display wired to `useJobProgress` with polling fallback — makes T-113 green | `web/app/cv/ingest/page.tsx` (or equivalent route), `web/lib/api.ts` (add `postIngest`/`getIngestion` API client calls) |
| T-115 | verify | Run `cd web && npm test -- --run` to confirm the full web suite (existing + new hook/page tests) is green | n/a (verification step) |

**Sequencing within E:** T-111 → T-112 → T-113 → T-114 → T-115, strictly sequential (UI test/impl depends on the hook existing).

---

## Cross-seam end-to-end verification (after all seams land)

| ID | Type | Description |
|----|------|--------------|
| T-116 | verify | Run `make test-all` (full suite: Go + worker + web + pgTAP RLS) to confirm no cross-seam regression once A through E are all merged |

---

## Review Workload Forecast

| Seam | Hand-written lines (impl + test) | Notes |
|------|-----------------------------------|-------|
| A — DB schema + migration + sqlc | ~70 (+ generated sqlc, not counted) | migration ~25, schema.sql ~15, rls.sql ~4, queries ~25; pgTAP test ~40-60 separately tracked in test budget below |
| B — Go API endpoints | ~120 (handler ~45, service ~55, interface/errors ~12, imports ~8) | tests tracked separately |
| C — Worker job + pdf fix | ~140 (ingest-cv.mjs ~75, ingest-prompt.mjs ~40, anthropic.mjs +10, index.mjs +3, pdf.mjs ~1) | tests tracked separately |
| D — WS rename (atomic) | ~6 (listener.go ~4, progress.mjs ~2) | tiny by design; risk is correctness, not size |
| E — Web hook + UI | ~120 (useJobProgress.ts ~110 new, page/API client ~10+) | tests tracked separately; actual UI page may run larger depending on design fidelity |
| Tests (Go + worker + pgTAP + web, all seams) | ~250 | spread across A (~50 pgTAP), B (~80 Go), C (~80 worker), E (~40 web) |
| **Total (hand-written, excl. generated sqlc)** | **~706** | |

**Chained PRs recommended: Yes**
**400-line budget risk: High**
**Decision needed before apply: Yes**

### Recommended PR sequence (dependency edges)

```
PR-A (DB+sqlc, ~70+50 test = ~120 lines)
   │
   ├──► PR-B (Go API+tests, ~120+80 test = ~200 lines)   [depends on PR-A: generated types]
   │
   ├──► PR-D (WS rename, atomic, ~6 lines)                [independent of PR-A; can land in parallel with PR-A/B]
   │        │
   │        └──► PR-C (worker+pdf+tests, ~140+80 test = ~220 lines)  [depends on PR-A: table exists; depends on PR-D: NOTIFY field name]
   │                  │
   │                  └──► PR-E (web hook+UI+tests, ~120+40 test = ~160 lines)  [depends on PR-D: run_id field; depends on PR-B: endpoints exist]
```

Each individual PR lands under the 400-line budget (largest is PR-C at ~220 lines). PR-D is small enough to land standalone immediately, removing it from the critical path risk even though it gates PR-C.

**Sequencing rationale:**
1. **PR-A first** — nothing else compiles/runs without the generated sqlc types and the `cv_ingestions` table.
2. **PR-D can land in parallel with PR-A/PR-B** — it's a pure rename touching only WS plumbing, has zero dependency on the new table, and its small size (~6 lines) makes it low-risk to land early and get out of the critical path.
3. **PR-B next** (or parallel with PR-D) — depends only on PR-A.
4. **PR-C after PR-A and PR-D** — the worker job's `notify()` call must be written against the already-renamed `run_id` field (chosen sequencing from Seam C's note: no temporary/dual-emit code, write directly against the final field name).
5. **PR-E last** — the hook needs both the renamed field (PR-D) and the live endpoints (PR-B) to be meaningfully testable end-to-end, though it could be developed against mocks earlier if desired.

### Recommended chain strategy

**Stacked-to-main** is the better fit for this change: PR-A, PR-D, PR-B, PR-C, PR-E are each independently mergeable and individually useful (PR-A alone ships a dormant table; PR-D alone is a no-op rename; PR-B alone exposes endpoints with a job that doesn't exist yet, which is acceptable since pg-boss just queues until a worker registers; only PR-C wires the actual processing). None of the seams require a multi-PR feature flag or a "do not merge until everything is ready" tracker — each slice degrades gracefully if the next slice is delayed. **Feature-branch-chain** would be the right call only if the team needs `POST /api/cv/ingest` to be invisible/disabled until the full pipeline (through PR-E) is ready — e.g., if accidentally exposing a 202-but-never-processed endpoint in production is unacceptable. Final choice is left to the orchestrator/user per `ask-on-risk`.

---

## Traceability summary

| Spec Requirement | Task IDs |
|-------------------|----------|
| Req 1 — `POST /api/cv/ingest` enqueues | T-85, T-86, T-89, T-90, T-93 |
| Req 2 — `GET /api/cv/ingest/:id` status | T-87, T-88, T-91, T-92, T-93 |
| Req 3 — Worker job processing | T-95, T-96, T-97, T-98, T-99, T-100, T-101, T-102, T-103, T-104 |
| Req 4 — `profile_json` nested schema + pdf fix | T-105, T-106 |
| Req 5 — WS progress delivery | T-108, T-109, T-110, T-111, T-112 |
| Req 6 — DB invariants (RLS, usage accounting) | T-79, T-80, T-81, T-82, T-83, T-84 |
| UI wiring (proposal success criteria) | T-113, T-114 |
| Cross-seam regression safety | T-94, T-107, T-115, T-116 |

## Next step

Run `sdd-apply` with the resolved `delivery_strategy` (currently `ask-on-risk` per orchestrator context) and, once a chain strategy is confirmed by the user, implement seams in the dependency order above: A → (D parallel) → B → C → E.
