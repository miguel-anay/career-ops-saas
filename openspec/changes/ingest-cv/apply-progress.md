# Apply Progress — `ingest-cv`

> Batches: 1+2 (Seam A, branch `feat/ingest-cv-db`) · 3 (Seam B, branch `feat/ingest-cv-api`) · 4 (Seam E, branch `feat/ingest-cv-web`)
> Strict TDD: active (RED -> GREEN per task)
> Seam A status: **COMPLETE**. Seam B status: **code-complete, RLS-engaged, unit-green; NOT yet DB-verified** (orchestrator must run the integration test against a live DB — see Batch 3 section below). Seam E status: **COMPLETE**.
> Seam C/D not started. Do not start them from this record without a fresh tasks read.

## Task status

| ID | Description | Status | Commit |
|----|--------------|--------|--------|
| T-79 | pgTAP RLS test `db/tests/cv_ingestions_rls.test.sql` | done | `907dfe2` (test), revised in `8a9e048` |
| T-80 | `db/migrations/002_ingest_cv.sql` — `cv_ingestions` table + RLS + `usage.ingestions_count` | done | `274ba2c` |
| T-81 | Mirror DDL into `db/schema.sql` + `db/rls.sql` | done | `b60cff5` |
| T-82 | sqlc queries: `cv_ingestions.sql` (Insert/Get/UpdateStatus) + `usage.sql` (`UpsertIncrementIngestions`) | done | `f90d415` |
| T-83 | Regenerate sqlc Go types (`CvIngestion` struct, query methods, `Usage.IngestionsCount`) | **done** | `614b61d` |
| T-84 | Run `make test-rls`, confirm T-79 passes against the live migration | **done** | n/a (verification only) — see Batch 2 below |
| — | Fix legacy `db/tests/rls_test.sql` false-positive (user-approved, folded into Seam A) | **done** | `876643e` |

## Batch 2 — close-out (this session)

**Unblocked T-83**: the `api/internal/db/` file ownership issue from Batch 1 was resolved externally (files are
now `k3n5h1n:k3n5h1n`, writable). Ran `sqlc generate` from `db/` with the same binary
(`/home/k3n5h1n/gopath/bin/sqlc`, v1.31.1) used in Batch 1. Clean exit, no errors. Generated:
- `api/internal/db/cv_ingestions.sql.go` (new) — `CvIngestion` struct usage via `GetCVIngestion`,
  `InsertCVIngestion`, `UpdateCVIngestionStatus` (+ `UpdateCVIngestionStatusParams`).
- `api/internal/db/models.go` (modified) — added `CvIngestion` struct, added `Usage.IngestionsCount` field.
- `api/internal/db/usage.sql.go` (modified) — `UpsertIncrementIngestions` + `UpsertIncrementIngestionsParams`,
  and the `IngestionsCount` field threaded through all three existing `usage` query scans.

Verified `cd api && go build ./...` (clean) and `go test ./... -count=1` (all packages pass, no regressions).
Committed as `614b61d` — `chore(db): regenerate sqlc types for cv_ingestions`.

**Fixed the legacy `db/tests/rls_test.sql` false positive** (user-approved scope addition, still Seam A —
this is the DB/RLS layer, not Seam B/C/D/E):
- Root cause #1: ran as `careerops` (the `POSTGRES_USER`, a Postgres superuser). Superusers bypass RLS
  unconditionally regardless of `FORCE ROW LEVEL SECURITY` — every cross-tenant assertion in the file was a
  false positive that happened to read `0` rows by coincidence of context, not because RLS was enforced.
- Root cause #2: asserted on `pg_class.rowsecurity`, which does not exist on PostgreSQL 16 (correct column:
  `relrowsecurity`). This made the file fail outright (24/24 subtests) before it could even reach the
  superuser-bypass problem.
- Root cause #3 (discovered while fixing, not previously documented): `plan(24)` undercounted — the file has
  always contained 25 `ok()`/`is()` assertions, not 24. This was masked previously because execution died on
  assertion #1 before the plan-mismatch could surface. Fixed to `plan(25)`.
- Fix applied: seed both tenant users via `auth_upsert_user` (SECURITY DEFINER, bypasses RLS for setup exactly
  as production OAuth signup does) instead of a direct `INSERT INTO users` with hardcoded UUIDs — a direct
  insert for "user B" while `app.current_user_id` is set to user A would violate the `tenant_users` `WITH CHECK`
  policy under `app_user`. Captured the generated UUIDs via `set_config('test.user_a'/'test.user_b', ..., false)`
  and used them as FKs for all other seeded rows (watched_companies, jobs, applications, reports, cvs,
  scan_runs, usage — those tables' own PKs stay as the original hardcoded literals, only the `user_id` FK
  changed). Switched every `pg_class` assertion from `rowsecurity` to `relrowsecurity`. Switched the
  `SET app.current_user_id = '...'` literal-role-switch lines to `set_config` calls (wrapped in `DO $$ BEGIN
  PERFORM ... END $$;` to avoid emitting a spurious extra TAP line — a bare top-level `SELECT set_config(...)`
  is counted by pg_prove/psql's TAP harness as its own subtest result line).
- Verified RED first: ran the unmodified file as `careerops` — confirmed `Failed 24/24 subtests`,
  `ERROR: column "rowsecurity" does not exist`. This is the evidence the test was broken.
- Verified GREEN after: ran as `app_user` — `Tests=25 ... Result: PASS`, all genuine RLS assertions.
- Updated `Makefile`'s `test-rls` target: `rls_test.sql` now runs via the same `app_user`/`PGPASSWORD=app_pw`
  `pg_prove` invocation pattern as `cv_ingestions_rls.test.sql` (was `-U careerops`).
- Committed as `876643e` — `fix(db): run legacy RLS pgTAP test as app_user and use relrowsecurity`.

**Final verification — full `make test-rls` target, clean run from a stopped container:**
```
docker compose exec ... pg_prove -U app_user -d careerops /db/tests/rls_test.sql
  /db/tests/rls_test.sql .. ok
  All tests successful.
  Files=1, Tests=25,  Result: PASS

docker compose exec ... pg_prove -U app_user -d careerops /db/tests/cv_ingestions_rls.test.sql
  /db/tests/cv_ingestions_rls.test.sql .. ok
  All tests successful.
  Files=1, Tests=4,  Result: PASS
```
Target exits 0 end-to-end (no longer halts on the legacy file). `make test-go` also reconfirmed green after the
sqlc regeneration (11 Go packages, all `ok` or no-test-files, zero failures).

## Deviations from the plan

1. **T-79 test rewritten to seed users via `auth_upsert_user` (SECURITY DEFINER) instead of a direct `INSERT INTO users`.**
   Discovery: the Docker Postgres role `careerops` (`POSTGRES_USER`) is a **superuser**, and Postgres superusers
   unconditionally bypass RLS regardless of `FORCE ROW LEVEL SECURITY`. Running the pgTAP assertions as `careerops`
   would be a false positive — it never exercises the policy. The test now seeds the two tenant users through the
   `auth_upsert_user` SECURITY DEFINER helper (mirrors production OAuth signup) and runs entirely as `app_user`,
   the real RLS-enforced runtime role. Verified standalone:
   ```
   docker compose exec -T -e PGPASSWORD=app_pw postgres pg_prove -U app_user -d careerops /db/tests/cv_ingestions_rls.test.sql
   # Files=1, Tests=4, Result: PASS
   ```

2. **`docker-compose.yml` and `Makefile` updated (infra fix, in scope for T-84 to be meaningful).**
   - Added a bind mount `./db/tests:/db/tests` on the `postgres` service — previously no test files were reachable
     inside the container at all except via `docker cp`.
   - `Makefile`'s `test-rls` target used `docker compose run --rm ...` for the pgtap-extension-create and pg_prove
     steps. `run` spins a brand-new ephemeral container from the base image, which never has the `pgtap`/`pg_prove`
     packages that an earlier `apt-get install` (via `exec`, against the long-lived service container) put in place.
     Changed both steps to `exec` against the running `postgres` service.
   - `Makefile`'s `apt-get install -y -qq pgtap` resolves to `postgresql-18-pgtap` on this image's apt mirror, even
     though the running server is PostgreSQL 16 — the extension control file then doesn't exist for PG16 and
     `CREATE EXTENSION pgtap` fails. Pinned to `postgresql-16-pgtap` explicitly.
   - Added an explicit apply of `002_ingest_cv.sql` before running tests (the existing line only re-applies
     `001_initial.sql`).
   - Split the `pg_prove` invocation. NOTE (corrected): at Batch 1 time the legacy `rls_test.sql` was left running
     as `careerops`; this was **subsequently changed in the review-fix batch** (see Blockers → T-84 below) to run as
     `app_user` so it genuinely exercises RLS. The new `cv_ingestions_rls.test.sql` runs as `app_user` via a second
     `pg_prove` call with `PGPASSWORD=app_pw`.

## Blockers (Batch 1) — both resolved in Batch 2

### T-83 — sqlc Go code generation blocked by file ownership — RESOLVED

Originally blocked: `api/internal/db/*.go` were owned `root:root` (leftover from an earlier `docker compose` run
that wrote there as root), and this session's user had no way to `chown`/`chmod` them without an interactive
sudo password. **Resolution (Batch 2)**: ownership was fixed externally to `k3n5h1n:k3n5h1n` before this batch
started. Verified writable, ran `sqlc generate` cleanly, regenerated `CvIngestion`, `InsertCVIngestion`,
`GetCVIngestion`, `UpdateCVIngestionStatus`, `UpsertIncrementIngestions`, `Usage.IngestionsCount`. `go build ./...`
and `go test ./... -count=1` both green. Committed as `614b61d`. Seam B is now unblocked.

### T-84 — `make test-rls` fails as a whole (pre-existing bug in `rls_test.sql`) — RESOLVED

Originally: the full `make test-rls` target halted on the pre-existing `db/tests/rls_test.sql` (`pg_class.rowsecurity`
does not exist on PG16 — correct column is `relrowsecurity`), and even after that fix the file ran as `careerops`
(superuser), which bypasses RLS unconditionally — a false positive. **Resolution (Batch 2)**: user explicitly
approved folding this fix into Seam A/PR-A (see task instructions). Fixed `rowsecurity` -> `relrowsecurity`,
switched seeding to `auth_upsert_user` (SECURITY DEFINER) so the whole file can run as `app_user`, fixed a
previously-undiscovered `plan(24)` vs. actual-25-assertions mismatch, and updated the `Makefile` to invoke
`pg_prove -U app_user` for `rls_test.sql` (was `-U careerops`). Verified RED (24/24 fail, `rowsecurity` error)
before the fix and GREEN (25/25 pass) after, both as evidence in this same session. Committed as `876643e`.
`make test-rls` as a whole now exits 0.

## Files changed (Batch 1 + Batch 2, cumulative)

- `db/tests/cv_ingestions_rls.test.sql` (new, then revised) — Batch 1
- `db/migrations/002_ingest_cv.sql` (new) — Batch 1
- `db/schema.sql` (mirrored DDL) — Batch 1
- `db/rls.sql` (mirrored DDL) — Batch 1
- `db/queries/cv_ingestions.sql` (new) — Batch 1
- `db/queries/usage.sql` (extended) — Batch 1
- `Makefile` (test-rls target fix — infra in Batch 1, app_user switch for legacy test in Batch 2) — Batch 1 + 2
- `docker-compose.yml` (postgres service: added `db/tests` mount) — Batch 1
- `api/internal/db/cv_ingestions.sql.go` (new, sqlc-generated) — Batch 2
- `api/internal/db/models.go` (sqlc-generated: `CvIngestion` struct, `Usage.IngestionsCount`) — Batch 2
- `api/internal/db/usage.sql.go` (sqlc-generated: `UpsertIncrementIngestions`) — Batch 2
- `db/tests/rls_test.sql` (fixed: `relrowsecurity`, `app_user` seeding via `auth_upsert_user`, `plan(25)`) — Batch 2

## Commits (in order)

1. `907dfe2` — `test(db): pgTAP RLS for cv_ingestions`
2. `274ba2c` — `feat(db): cv_ingestions table + usage.ingestions_count migration`
3. `8a9e048` — `fix(db): run pgTAP cv_ingestions test as RLS-enforced app_user`
4. `b60cff5` — `feat(db): mirror cv_ingestions DDL into schema.sql and rls.sql`
5. `f90d415` — `feat(db): sqlc queries for cv_ingestions`
6. `614b61d` — `chore(db): regenerate sqlc types for cv_ingestions`
7. `876643e` — `fix(db): run legacy RLS pgTAP test as app_user and use relrowsecurity`

## Seam A status: COMPLETE

All of T-79..T-84 are done, plus the user-approved legacy `rls_test.sql` fix. `make test-rls` (25/25 + 4/4) and
`make test-go` (11 packages, all pass) are both green end-to-end on branch `feat/ingest-cv-db`. Not pushed, no PR
opened — orchestrator reviews first per task constraints.

## Batch 3 — Seam B (Go API ingest/status endpoints), branch `feat/ingest-cv-api`

Seam B's Go code (T-85..T-94, `handler.go`/`service.go`/`handler_test.go`) was already written with 23 green unit
tests, but those mock the `Servicer` — they never exercised RLS. Root cause found: the API connects as `app_user`
(plain LOGIN role, no SUPERUSER/BYPASSRLS); `cv_ingestions` and `usage` both have FORCE+ENABLE RLS gated on
`current_setting('app.current_user_id')`; the only code that ever sets that var is `platform.WithTenant`, which no
service in the codebase calls, including `cv.Service` (`queries()` uses `stdlib.OpenDBFromPool` with the var never
set). As written, `EnqueueIngest` would fail the `WITH CHECK` on insert, and `GetIngestion` would 404 even for the
owner.

**Fix (Seam-B-scoped only)**: added `withTenant(ctx, userID, fn)` helper to `api/internal/cv/service.go` that opens
a `*sql.Tx` via `stdlib.OpenDBFromPool`, runs `SELECT set_config('app.current_user_id', $1, true)` (parameterized,
transaction-local), then `fn(db.New(tx))`, then commits. Rewrote `EnqueueIngest` to run usage-check +
`InsertCVIngestion` + `UpsertIncrementIngestions` inside ONE `withTenant` transaction (atomic, RLS-engaged);
`queue.Enqueue` stays outside (after commit) since `pgboss.job` has no RLS policy — an orphaned pending row on
enqueue failure is accepted as MVP tradeoff. Rewrote `GetIngestion` to call `GetCVIngestion` inside `withTenant` —
non-owner lookups now genuinely return `sql.ErrNoRows` because RLS hides the row, not via any app-layer check.

**Usage increment / spec-design conflict resolved**: spec Req 6 says `ingestions_count` increments "on enqueue";
design.md's worker pseudocode (`handleIngestCV` step 6) also showed the UPSERT happening in the worker — that
would double count. Resolution: increment happens ONLY at enqueue time (in `EnqueueIngest`, via
`UpsertIncrementIngestions` inside the same tenant tx). **Seam C's future T-102 (`handleIngestCV`) MUST NOT also
increment usage** — worker should only write `users.cv_markdown`/`profile_json` and transition
`cv_ingestions.status`.

**Codebase-wide RLS gap — recorded as follow-up, NOT fixed**: `platform.WithTenant` is dead code unused anywhere
else. `evaluate/service.go:37` and `scan/service.go:26` have the identical pattern (`stdlib.OpenDBFromPool` with
tenant var never set) — meaning `evaluate` and `scan` (and likely `companies`/`tracker`/`jobs`) currently run all
DB access with `app.current_user_id` unset. Pre-existing, codebase-wide issue outside `ingest-cv` scope; recommend
a dedicated follow-up SDD change (e.g. `fix-tenant-rls-engagement`).

Tests: new `api/internal/cv/ingest_integration_test.go` — DB-gated (skips cleanly if `TEST_DATABASE_URL` unset),
connects via `platform.NewPool`, seeds 2 users via `auth_upsert_user` (SECURITY DEFINER, bypasses RLS for setup
like production OAuth). 5 subtests: owner enqueue creates row + increments usage to 1; second enqueue increments
to 2 while `evaluations_count` stays unaffected; first-ingestion-of-month-with-no-usage-row succeeds at count 1;
limit gating blocks the 6th call with `ErrUsageLimitExceeded` and does not create a row or increment past 5; RLS
isolation — owner `GetIngestion` succeeds, non-owner `GetIngestion` on the same `run_id` returns `ErrNotFound`.

Verification done (no live DB, per task constraint): `go build ./...` clean; `go vet ./...` clean;
`go test ./internal/cv/... -count=1 -v` — 23 unit tests PASS, `TestCVIngest_RLS_Integration` SKIPs cleanly; full
`go test ./... -count=1` — all 11 packages pass, zero regressions.

**Command for orchestrator to run against a live DB** (matches `docker-compose.yml`'s `app_user`/`app_pw`):
```
docker compose up -d postgres
TEST_DATABASE_URL="postgres://app_user:app_pw@localhost:5432/careerops?sslmode=disable" \
  go test ./internal/cv/... -run TestCVIngest_RLS_Integration -v -count=1
```
(run from `api/`; DB needs `001_initial.sql` + `002_ingest_cv.sql` applied)

Files changed (Batch 3): `api/internal/cv/service.go` (modified — `withTenant` helper, rewrote
`EnqueueIngest`/`GetIngestion`), `api/internal/cv/ingest_integration_test.go` (new). `handler.go`/`handler_test.go`
were already final from before this batch, untouched in Batch 3.

**Status**: Seam B code-complete + RLS-engaged + unit-green, but NOT YET VERIFIED against a live DB — orchestrator
must run the integration test command above.

## Batch 4 — Seam E (web hook `useJobProgress` + UI), branch `feat/ingest-cv-web`

**Strict TDD followed**: RED confirmed for both the hook test (import-not-found) and the page test
(import-not-found) before any implementation file was written.

**T-111/T-112 — `useJobProgress` hook** (`web/hooks/useJobProgress.ts`, test
`web/__tests__/hooks/useJobProgress.test.tsx`, 7 tests): generalized clone of `useScanProgress.ts`. Status enum
`idle|connecting|working|completed|error`. `connect(runId)` builds the WS URL via
`URLSearchParams({ scan_run_id: runId })` + `token` — reuses the existing `/ws/scan` route and the existing
`scan_run_id` query-param name verbatim (per task instruction: only the envelope event names differ, not the
param). Matches incoming envelope on `ingest.completed` (→ `completed`, payload surfaced via `setPayload`) and
`ingest.failed` (→ `error`, payload surfaced). Reconnect-once: close while not in a terminal state schedules
exactly one retry via `setTimeout(() => doConnect(runId), 1000)`.

**Bug found and fixed during TDD (not present in `useScanProgress`'s simpler case)**: a naive port where the
reconnect timer called the public `connect(runId)` re-armed `reconnectAttempted.current = false` on every retry,
allowing unlimited reconnects instead of exactly one. Fixed by splitting a private `doConnect(runId)` (the actual
WS-opening logic, does NOT touch the reconnect-attempted flag) from the public `connect(runId)` (resets the flag,
then calls `doConnect`) — only `doConnect` is what the `onclose` retry timer calls. Caught by the
"reconnects once after an unexpected close" test, which asserts a 2nd close produces no 3rd WS instance.

Two-different-`run_id`s-do-not-cross-deliver verified by rendering two independent hook instances connected to
different `run_id`s and asserting an event sent to one connection's mock WS does not change the other's status/
payload.

**T-113/T-114 — Paste-CV UI**: `web/app/cv/ingest/page.tsx` (new route) + test
`web/__tests__/app/ingest-cv.test.tsx` (6 tests). Textarea (placeholder "Paste your CV text here…") + Submit
button (disabled when empty/whitespace-only or submitting) wired to `postIngest`/`getIngestion`, newly added to
`web/lib/api.ts`:
- `postIngest(rawCV: string): Promise<{run_id: string}>` → `apiPost('/api/cv/ingest', {raw_cv: rawCV})`
- `getIngestion(runId: string): Promise<IngestionStatus>` → `apiGet('/api/cv/ingest/${runId}')`
- `IngestionStatus` shape: `{id, status: 'pending'|'processing'|'completed'|'failed', started_at, finished_at}`

On submit: calls `postIngest(rawCV.trim())`, stores `run_id`, calls `connect(run_id)` from `useJobProgress()`.
Live status renders from hook `status`/`payload` ("Processing your CV…" while `working`, "Completed"/"Failed"
labels + JSON payload dump on terminal states). **Polling fallback**: a `useEffect` keyed on
`[runId, status, isConnected, stopPolling]` starts a `setInterval(..., 4000)` calling `getIngestion(runId)` only
when a run is in flight (`status` not idle/completed/error) AND the WS is NOT connected (`isConnected === false`);
stops the interval once polling itself observes a terminal `completed`/`failed` status, or once the hook's own
`status` reaches a terminal state, or once the WS reconnects (`isConnected` flips true). An `effectiveStatus`
derived value lets a polling-discovered terminal state override the hook's possibly-stuck `working` status when
the WS never recovers.

**Test-only fix**: the "completed" page test originally asserted `screen.getByText(/completed/i)` which matched
both the "Completed" label AND the JSON-dumped `"status": "completed"` string in the same DOM tree (multiple
elements found). Fixed the test assertion to the exact string `screen.getByText('Completed')` rather than
weakening the component — the JSON payload dump is intentional UI behavior (surfaces the full terminal payload
per spec Req 5).

**Files changed (Batch 4)**:
- `web/hooks/useJobProgress.ts` (new)
- `web/__tests__/hooks/useJobProgress.test.tsx` (new, 7 tests)
- `web/app/cv/ingest/page.tsx` (new)
- `web/__tests__/app/ingest-cv.test.tsx` (new, 6 tests)
- `web/lib/api.ts` (modified — added `postIngest`, `getIngestion`, `IngestRunResponse`, `IngestionStatus`)
- `useScanProgress.ts` and the scan flow were NOT touched, per task constraint.

**Verification**: `cd web && npm test -- --run` → **7 test files passed, 30 tests passed** (was 5 files/17 tests
before this batch; this batch added 2 files/13 tests: 7 hook + 6 page). `npx tsc --noEmit` clean. `npx next lint`
clean — no warnings or errors. No test left RED.

**Status**: Seam E **COMPLETE** (T-111..T-115 all done) on branch `feat/ingest-cv-web`. Not committed, not
pushed, no PR opened — orchestrator reviews first per task constraints.

## Next steps (Seam C/D — NOT started)

1. Seam D (WS field rename `scan_run_id`→`run_id`, atomic) has no code dependency on Seam E and can land anytime;
   Seam E's hook already reads the envelope key `run_id` per the task contract, so once Seam D actually lands in
   `worker/lib/progress.mjs`/`api/internal/ws/listener.go`, Seam E requires no changes.
2. Seam C (worker `ingest-cv` job) still depends on Seam D landing first for the NOTIFY field name (see Seam C's
   sequencing note in `tasks.md`) and on Seam A (table exists).
3. Do not start Seam C/D from this apply-progress record without first re-reading the `tasks` artifact.
