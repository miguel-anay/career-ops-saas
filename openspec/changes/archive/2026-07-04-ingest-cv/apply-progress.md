# Apply Progress — `ingest-cv`

> Batch: 1 (Seam A only — DB schema + migration + sqlc) + Batch 2 (T-83/T-84 close-out + legacy RLS fix)
>          + Batch 3 (Seam B RLS-engagement fix — T-85..T-94 code already written, RLS gap closed)
> Branch: `feat/ingest-cv-db` (Seam A) / `feat/ingest-cv-api` (Seam B)
> Strict TDD: active (RED -> GREEN per task, sequential T-79 -> T-84; Batch 3 is a targeted RLS fix on
> already-implemented Seam B code, verified via a new DB-gated integration test)
> Seam A status: **COMPLETE**.
> Seam B status: **CODE COMPLETE + RLS-ENGAGED**, integration test compiles/skips cleanly; **NOT YET VERIFIED
> against a live DB** (orchestrator must run the integration test — see Batch 3 below). Do not start Seam C/D/E
> from this record without a fresh tasks read.

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

## Batch 3 — Seam B RLS-engagement fix (this session)

### Context: what was broken (orchestrator-verified before this batch)

Seam B's Go code (T-85..T-94) was already written and 23 handler/service unit tests were green, but those
tests mock the `Servicer`/queries layer — they prove nothing about RLS. The Go API connects as `app_user`
(`docker-compose.yml` `DATABASE_URL`), a plain LOGIN role with **no SUPERUSER/BYPASSRLS** and not the table
owner. `cv_ingestions` and `usage` both have `FORCE` + `ENABLE ROW LEVEL SECURITY` with policies gated on
`current_setting('app.current_user_id', true)::uuid`. The only code that ever set that session variable was
`platform.WithTenant` — which **no handler or service in the entire codebase calls**, including `cv.Service`.
`cv.Service.queries()` opens a raw `*sql.DB` via `stdlib.OpenDBFromPool` with the variable never set. Net
effect, as written: `EnqueueIngest` would have failed on `InsertCVIngestion`'s `WITH CHECK`, and `GetIngestion`
would 404 even for the rightful owner (RLS `USING` clause sees `current_user_id` as NULL/unset, which never
equals any `user_id`).

### Decision (user-directed): fix RLS locally in the cv ingest path only

Did **not** refactor `platform.WithTenant`/`middleware.TenantIsolation` or touch any other domain
(`evaluate`, `scan`, `jobs`, `companies`, `tracker`). This is a **Seam-B-scoped** fix.

**Codebase-wide RLS gap — recorded as a follow-up, NOT fixed here:** `platform.WithTenant` exists but is
dead code (unused outside its own file). Every other domain's `Service.queries()` method has the exact same
gap as `cv` did — `evaluate/service.go:37` and `scan/service.go:26` both call
`stdlib.OpenDBFromPool(s.pool)` directly with no tenant variable set, same pattern this batch just fixed
locally in `cv`. **This means `evaluate` and `scan` (and likely `companies`/`tracker`/`jobs`) currently run
all DB access with `app.current_user_id` unset**, so their RLS policies are either silently denying rows
(if `USING` requires equality with a set value) or — depending on policy wording — potentially permissive in
ways not yet audited. This is a pre-existing, codebase-wide issue **outside this change's scope**; flagging
for a dedicated follow-up SDD change (something like `fix-tenant-rls-engagement`) rather than fixing
opportunistically here, since the scope here is "finish Seam B of ingest-cv," not "harden tenancy globally."

### What was implemented

**1. `withTenant` helper added to `api/internal/cv/service.go`:**
```go
func (s *Service) withTenant(ctx context.Context, userID uuid.UUID, fn func(q *db.Queries) error) error {
    sqlDB := stdlib.OpenDBFromPool(s.pool)
    tx, err := sqlDB.BeginTx(ctx, nil)
    if err != nil { return fmt.Errorf("begin tenant tx: %w", err) }
    defer func() { _ = tx.Rollback() }()
    if _, err := tx.ExecContext(ctx, "SELECT set_config('app.current_user_id', $1, true)", userID.String()); err != nil {
        return fmt.Errorf("set tenant user: %w", err)
    }
    if err := fn(db.New(tx)); err != nil { return err }
    return tx.Commit()
}
```
Uses `set_config(..., true)` (transaction-local, parameterized — no string interpolation/injection risk),
matching the literal pattern requested. This is additive to the `cv` package only; `queries()` (used by the
4 pre-existing `cv` methods: `EnqueuePDFGeneration`, `GetDownloadURL`, `ListCVs`, `CreateCV`, `SetMasterCV`)
is untouched and still does NOT set the tenant variable — those methods were out of scope for this batch
(they predate ingest-cv and were not part of T-85..T-94).

**2. `EnqueueIngest` rewritten** to run the usage-check + `InsertCVIngestion` + `UpsertIncrementIngestions`
inside ONE `withTenant` transaction — atomic, RLS-engaged. The pg-boss `queue.Enqueue` call stays OUTSIDE
that transaction (after commit), using the plain pool, because `pgboss.job` has no RLS policy and is not a
tenant table. If enqueue fails after the tenant tx commits, the `cv_ingestions` row + usage increment persist
(orphaned `pending` row) — accepted as an MVP tradeoff per the task instructions, not changed.

**3. `GetIngestion` rewritten** to call `GetCVIngestion` inside `withTenant`. Removed reliance on any
app-layer ownership check (there never was one in the existing code — confirmed by reading the pre-batch
`service.go`, it already relied on RLS-as-designed; the bug was that RLS was never actually engaged). Now
that the lookup runs inside a transaction with `app.current_user_id` set, a non-owner's lookup genuinely
returns `sql.ErrNoRows` (mapped to `cv.ErrNotFound`) **because RLS hides the row**, not because of a
conditional in Go.

**4. Usage increment (Req 6) wired into `EnqueueIngest`** via `q.UpsertIncrementIngestions` inside the same
tenant tx, right after `InsertCVIngestion` succeeds and before the function returns the `run_id`. This
resolves the spec/design conflict flagged in the task brief: design.md's worker pseudocode (§3.1, step 6 in
the diagrams) also showed the UPSERT happening in `handleIngestCV`/T-102 — that would double-count
`ingestions_count` (once at enqueue, once at job completion). **Resolution recorded here: the increment
happens at ENQUEUE time only (in the Go API, this batch). When Seam C is implemented, T-102's
`handleIngestCV` implementation MUST NOT call `UpsertIncrementIngestions` (or any other usage-incrementing
write) — the worker should only write `users.cv_markdown`/`profile_json` and transition
`cv_ingestions.status`, never touch `usage`.** This is a deviation from design.md's literal diagram/pseudocode
(§Decisions-at-a-glance picture, step 6, and §3.1's `handleIngestCV` code block step labeled "// 3. meter
usage") — the spec (Req 6, "ingestions_count increments on enqueue") is authoritative over the design
diagram here, since the spec scenario is explicitly named "increments on enqueue," not "increments on
completion."

### Tests written — `api/internal/cv/ingest_integration_test.go` (new)

DB-gated integration test, `TestCVIngest_RLS_Integration`, skips via
`if dsn := os.Getenv("TEST_DATABASE_URL"); dsn == "" { t.Skip(...) }`. Connects via `platform.NewPool`.
Scenarios (subtests via `t.Run`):
- `owner enqueue increments usage and creates a row` — calls real `EnqueueIngest`, asserts
  `usage.ingestions_count = 1` and the `cv_ingestions` row exists with the right `user_id`.
- `second enqueue increments counter independently of evaluations_count` — pre-seeds
  `evaluations_count = 3`, asserts a second `EnqueueIngest` brings `ingestions_count` to 2 while
  `evaluations_count` stays 3 (Req 6 distinct-counters scenario).
- `first ingestion of the month with no usage row succeeds and counts as zero baseline` — deletes the usage
  row first, asserts the call still succeeds and lands at count 1.
- `limit gating blocks the 6th enqueue and does not increment usage` — drives 5 successful enqueues to hit
  `freePlanIngestLimit`, then asserts the 6th returns `cv.ErrUsageLimitExceeded`, creates no new
  `cv_ingestions` row, and does not increment `ingestions_count` past 5.
- `RLS isolation: owner can read, non-owner gets ErrNotFound` — owner's `GetIngestion` succeeds; a second
  user's `GetIngestion` on the same `run_id` returns `cv.ErrNotFound`, proving RLS denial (not an app-layer
  `if`).

Users are seeded via `auth_upsert_user(email, google_id, NULL)` (SECURITY DEFINER, bypasses RLS for setup),
mirroring `db/tests/cv_ingestions_rls.test.sql`'s pattern. A `requireEnqueueSucceeded` helper tolerates an
enqueue-stage-only failure (message must contain `"enqueue ingest-cv"`) in case `pgboss.job` isn't installed
on a bare migrated DB — pg-boss creates its own schema at worker-runtime, which a fresh `001+002` migration
alone does not provide. All other errors fail the test outright. This keeps the RLS/usage assertions
(the actual point of the test) runnable even if pg-boss schema is missing, while still catching real
regressions.

Existing `handler_test.go` (23 tests, mocked `Servicer`) was left untouched — it already correctly covers
HTTP status/validation mapping and doesn't need DB access.

### Verification (this batch, no live DB used per task constraints)

```
cd api && go build ./...                          # clean, no output
cd api && go vet ./...                             # clean, no output
cd api && go test ./internal/cv/... -count=1 -v    # 23 unit tests PASS, integration test SKIP (clean)
cd api && go test ./... -count=1                   # all 11 packages pass, zero regressions
```

Integration test skip line confirmed: `ingest_integration_test.go:34: set TEST_DATABASE_URL to run cv ingest
integration tests` / `--- SKIP: TestCVIngest_RLS_Integration (0.00s)`.

### Command for the orchestrator to run the integration test against a live DB

Using the same `app_user`/`app_pw` credentials and port that `docker-compose.yml`'s API service uses
(non-superuser, RLS-enforced role — required for the test to be meaningful):

```
docker compose up -d postgres
# ensure 001_initial.sql + 002_ingest_cv.sql are applied (same as make test-rls does)
TEST_DATABASE_URL="postgres://app_user:app_pw@localhost:5432/careerops?sslmode=disable" \
  go test ./internal/cv/... -run TestCVIngest_RLS_Integration -v -count=1
```

Run from `api/`. Expect 5 passing subtests under `TestCVIngest_RLS_Integration` if RLS engagement is correct;
a 404-as-200 or a usage-count mismatch would indicate the tenant variable isn't being set as expected in that
environment.

### Files changed (Batch 3)

| File | Action | What |
|------|--------|------|
| `api/internal/cv/service.go` | Modified | Added `withTenant` helper; rewrote `EnqueueIngest` to run usage-check+insert+usage-increment in one tenant tx, enqueue after commit; rewrote `GetIngestion` to run `GetCVIngestion` inside a tenant tx |
| `api/internal/cv/ingest_integration_test.go` | Created | DB-gated integration test proving RLS engagement + usage accounting against a real `app_user` connection |

`api/internal/cv/handler.go` and `api/internal/cv/handler_test.go` were already in their final Seam-B state
from before this batch (uncommitted on `feat/ingest-cv-api`) — not modified in Batch 3, only read for context.

### Deviations from design (Batch 3)

1. **Usage increment location**: design.md shows `usage.ingestions_count` incremented in the worker
   (`handleIngestCV`, §3.1 step 6). Implemented instead at enqueue time in the Go API service (this batch),
   per spec Req 6's "increments on enqueue" scenario and explicit task instruction. **Seam C must not
   duplicate this increment** — see note above, this is the authoritative resolution.
2. **`GetIngestion`/`EnqueueIngest` now open a `*sql.Tx` per call** (via `withTenant`) instead of a bare
   `*sql.DB` query — necessary to scope `set_config(..., true)` (transaction-local) to the RLS-relevant
   queries. No interface/signature change; purely internal to `cv.Service`.
3. **Codebase-wide RLS gap not fixed** (see Decision above) — `platform.WithTenant` remains unused outside
   its own file; `evaluate`/`scan`/other domains still do not set `app.current_user_id`. Flagged as a
   follow-up, intentionally out of scope here.

### Status after Batch 3

Seam B (T-85..T-94) implementation is code-complete and unit-test-green (23/23), with the previously-missing
RLS engagement now wired in and a new integration test ready to prove it. **Not yet run against a live DB in
this batch** — orchestrator must execute the command above before Seam B can be considered verified end to
end. Seam C/D/E remain NOT started.
