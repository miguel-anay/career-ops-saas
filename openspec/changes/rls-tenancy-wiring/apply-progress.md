# Apply Progress — `rls-tenancy-wiring`

> Phase: apply · Status: Seam 1 (foundation) complete, Seam 2 (`scan`) complete, Seam 3 (`tracker`) complete, Seam 4 (`auth`) complete, Seam 5 (`jobs`) complete, Seams 6-8 not started
> Branch: `feat/rls-foundation` (Seam 1) → `feat/rls-scan` (Seam 2, based on `main` including Seam 1's foundation) → `feat/rls-tracker` (Seam 3, based on `main` including Seam 1's foundation AND Seam 2's `scan` slice) → `feat/rls-auth` (Seam 4, based on `main` including Seam 1+2+3) → `feat/rls-jobs` (Seam 5, based on `main` including Seam 1+2+3+4)
> Mode: Strict TDD (test runner: `make test-all` / `cd api && go test ./... -count=1`; `make test-rls` for pgTAP)
> Batch: 5 of N (Seam 5 — `jobs` slice)

## Seam 0 — Live-DB spike (D9) — already done before this batch

Per the orchestrator's brief, the spike was run prior to this apply batch and confirmed empirically against the live DB as `app_user`:

- GUC unset → `current_setting('app.current_user_id', true)` is `NULL` → clean deny (0 rows), no error.
- GUC = `''` (empty, the pooled-connection reset value after any tenant tx ends — `set_config(..., true)` is transaction-local) → `''::uuid` → `ERROR 22P02 invalid input syntax for type uuid: ""`.
- GUC = valid uuid → clean scoping.

This confirms the NULLIF hardening (Req 4) is both required and sufficient. It is the rationale baked into `db/migrations/003_rls_nullif.sql`'s header comment and into `db/tests/nullif_guc.test.sql`.

## Seam 1 — Foundation: NULLIF migration + `platform.WithTenantTx` + shared test harness

### TDD Cycle Evidence

| Task | RED | GREEN | REFACTOR |
|------|-----|-------|----------|
| T-118/T-119/T-121 (NULLIF migration + pgTAP) | Wrote `db/tests/nullif_guc.test.sql` asserting empty-GUC denies cleanly + properly-set-GUC happy path, BEFORE writing the migration | `db/migrations/003_rls_nullif.sql` added (DROP+CREATE all 9 policies with `NULLIF(...)::uuid`); pgTAP assertions designed to pass once migration is applied (not run live by `sdd-apply` — Docker is out of scope for this batch; orchestrator runs `make test-rls`) | n/a — single-pass SQL, no refactor needed |
| T-120 (`db/rls.sql` mirror) | n/a (DDL mirror, not test-driven) | `db/rls.sql` updated to match migration 003's policy bodies verbatim | n/a |
| T-123/T-124 (`platform.WithTenantTx`) | Wrote `api/internal/platform/postgres_test.go` (`TestWithTenantTx_RLS_Integration`) calling `platform.WithTenantTx` BEFORE it existed — confirmed RED via `go vet ./internal/platform/...` → `undefined: platform.WithTenantTx` | Implemented `WithTenantTx` in `api/internal/platform/postgres.go` (body lifted byte-for-byte from `cv.withTenant`) — `go build ./...` and `go vet ./...` clean | n/a — straight lift, no behavioral change from precedent |
| T-125 (delete dead `WithTenant`) | n/a (deletion, not test-driven; covered by T-130's build/test verification) | Removed `func WithTenant(...)` and the `github.com/jackc/pgx/v5` import (no longer used directly in `postgres.go` — only `pgxpool` and `stdlib` remain) | n/a |
| T-126 (delete `middleware/tenant.go` + mount) | n/a (deletion) | Deleted `api/internal/middleware/tenant.go`; removed `r.Use(middleware.TenantIsolation(pool))` from `api/cmd/api/main.go`. Grep-confirmed zero remaining references to `middleware.TenantIsolation` or `platform.WithTenant` (singular) anywhere in the Go tree | n/a |
| T-127/T-128 (re-point `cv.withTenant`) | n/a (refactor of already-tested code — safety net is the existing `cv` test suite, which must stay green) | Deleted `cv.withTenant`; re-pointed both call sites (`EnqueueIngest`, `GetIngestion`) at `platform.WithTenantTx(ctx, s.pool, userID, fn)` | Ran `cd api && go test ./internal/cv/... -count=1 -v` — all 21 existing tests pass unmodified, `TestCVIngest_RLS_Integration` skips cleanly without `TEST_DATABASE_URL` |
| T-129 (rlsdb harness) | Wrote `api/internal/testsupport/rlsdb/harness_test.go` (`TestHarness_WithTenantTx_CrossTenantDenial`, `TestHarness_EmptyGUC_DeniesCleanly`) alongside the harness — both skip cleanly without `TEST_DATABASE_URL`, confirmed via `go test ./internal/testsupport/... -v` | Implemented `api/internal/testsupport/rlsdb/harness.go` (`Harness{AppPool, AdminPool}`, `New`, `SeedUser`, `EnsurePgbossStandin`) generalized from `cv/ingest_integration_test.go` | n/a |
| T-130 (verify whole seam) | n/a | `go build ./...`, `go vet ./...`, `cd api && go test ./... -count=1` all green; `make test-go` run directly confirms the same | n/a |

### Files changed

| File | Action | What was done |
|------|--------|----------------|
| `db/migrations/003_rls_nullif.sql` | Created | DROP+CREATE all 9 tenant policies, `USING`/`WITH CHECK` hardened to `NULLIF(current_setting('app.current_user_id', true), '')::uuid`. ENABLE/FORCE flags untouched. |
| `db/rls.sql` | Modified | Mirrored the same 9 NULLIF-hardened policy bodies (bootstrap source of truth for a fresh `docker compose up`). |
| `db/tests/nullif_guc.test.sql` | Created | pgTAP: empty-GUC denies cleanly (no 22P02) + NULL-GUC still denies + properly-set GUC still scopes correctly (happy-path regression check). 4 assertions (`plan(4)`). |
| `Makefile` | Modified | `test-rls` target now re-applies `003_rls_nullif.sql` and runs `nullif_guc.test.sql` via `pg_prove`, alongside the existing `rls_test.sql` / `cv_ingestions_rls.test.sql`. |
| `api/internal/platform/postgres.go` | Modified | Added `WithTenantTx(ctx, pool, userID, fn) error`; deleted the dead `WithTenant(ctx, pool, userID string, fn func(*pgx.Conn) error)` and its now-unused `pgx/v5` import; added `db` + `uuid` + `stdlib` imports. |
| `api/internal/platform/postgres_test.go` | Created | `TestWithTenantTx_RLS_Integration` — DB-gated (skips without `TEST_DATABASE_URL`): proves GUC-scoped cross-tenant denial, commit-on-nil, rollback-on-error. |
| `api/internal/middleware/tenant.go` | Deleted | No-op `TenantIsolation` middleware removed (stale comment referenced the just-deleted `platform.WithTenant`; real tenancy mechanism is now `platform.WithTenantTx` at the service layer). |
| `api/cmd/api/main.go` | Modified | Removed `r.Use(middleware.TenantIsolation(pool))` mount line. |
| `api/internal/cv/service.go` | Modified | Deleted `cv.withTenant`; re-pointed `EnqueueIngest` and `GetIngestion` at `platform.WithTenantTx(ctx, s.pool, userID, fn)` directly. Doc comments updated to reference the shared helper instead of "Seam-B-local fix". |
| `api/internal/testsupport/rlsdb/harness.go` | Created | Shared DB-gated test harness: `Harness{AppPool, AdminPool}`, `New(ctx, t) *Harness` (t.Skip-gated), `SeedUser`, `EnsurePgbossStandin` — generalized from `cv/ingest_integration_test.go`. |
| `api/internal/testsupport/rlsdb/harness_test.go` | Created | `TestHarness_WithTenantTx_CrossTenantDenial` (proves the harness + `WithTenantTx` combo denies cross-tenant reads) and `TestHarness_EmptyGUC_DeniesCleanly` (Go-level mirror of the pgTAP empty-GUC assertion, exercised through `AppPool`). Both DB-gated, skip without `TEST_DATABASE_URL`. |
| `openspec/changes/rls-tenancy-wiring/tasks.md` | Modified | Marked T-118 through T-130 as done (T-122 and the `make test-rls` portion of T-130 deferred to the orchestrator's live-DB verification pass, per this batch's explicit instruction not to run Docker). |

### `platform.WithTenantTx` signature and location

```go
// api/internal/platform/postgres.go
func WithTenantTx(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, fn func(q *db.Queries) error) error
```

Body: `stdlib.OpenDBFromPool(pool)` → `sqlDB.BeginTx(ctx, nil)` → `tx.ExecContext(ctx, "SELECT set_config('app.current_user_id', $1, true)", userID.String())` → `fn(db.New(tx))` → `tx.Commit()` on nil, deferred `tx.Rollback()` otherwise. Byte-for-byte the proven `cv.withTenant` body, lifted one package level. Parameterized (`$1`) — no string interpolation, unlike the deleted `platform.WithTenant`.

### `rlsdb` harness package path and Seam 2-8 usage

Package: `api/internal/testsupport/rlsdb` (`package rlsdb`, consumed as `rlsdb_test` style by per-domain `*_test` packages, mirroring the convention already used by `cv_test`).

Each Seam 2-8 `{domain}/rls_integration_test.go` should:
1. `h := rlsdb.New(ctx, t)` at the top (skips cleanly without `TEST_DATABASE_URL`)
2. `userA := h.SeedUser(ctx, t, "...", "...")`, `userB := h.SeedUser(ctx, t, "...", "...")`
3. Seed ground-truth fixtures via `h.AdminPool` (bypasses RLS, exactly like the existing `cv` test's `adminPool`)
4. Call `h.EnsurePgbossStandin(ctx, t)` only if the domain's flow enqueues a pg-boss job (e.g. `scan`, `evaluate`, `cv`)
5. Exercise the `Service` under test (constructed with `h.AppPool`) and assert cross-tenant denial / owner-success per the spec scenarios in `design.md` §4.2

This keeps each per-domain test file to roughly 30-50 lines of assertions, per the design's stated goal (D7).

### Confirmation: `cv` re-pointed, dead code deleted

- `cv.withTenant` no longer exists; both of its call sites now call `platform.WithTenantTx` directly.
- `platform.WithTenant` (dead, zero callers, `fmt.Sprintf`-interpolated `SET LOCAL`) is deleted.
- `middleware.TenantIsolation` (no-op) is deleted, along with its mount in `api/cmd/api/main.go`.
- Grep-confirmed (`rg -n "platform\.WithTenant\b|middleware\.TenantIsolation|middleware/tenant" --type go .`) — zero remaining references anywhere in the Go tree.

### Deviations from design

None. Implementation matches `design.md` §1 (`WithTenantTx` shape), §3 (NULLIF migration), §4.1 (harness shape), and §6 (delete, not patch, `middleware/tenant.go`) exactly.

One clarification beyond what design explicitly specified: the foundation-level integration test proving empty-GUC denial (point 4 of the orchestrator's brief) was added as `TestHarness_EmptyGUC_DeniesCleanly` in the `rlsdb` package test file rather than as a new top-level test file, since it is a natural companion proof to the harness itself and exercises the same `AppPool` every domain test will use.

### Issues found

None.

### Verification run in this batch (no Docker / no live DB)

```
cd api && go build ./...        # clean
cd api && go vet ./...          # clean
cd api && go test ./... -count=1   # all packages PASS; 4 DB-gated tests SKIP cleanly:
  - platform: TestWithTenantTx_RLS_Integration
  - testsupport/rlsdb: TestHarness_WithTenantTx_CrossTenantDenial, TestHarness_EmptyGUC_DeniesCleanly
  - cv: TestCVIngest_RLS_Integration (pre-existing, still skips cleanly after the withTenant re-point)
make test-go   # same result, run via the Makefile target directly
```

### Exact commands the orchestrator should run for live verification

1. Recreate the DB with migration 003 applied (either fresh `docker compose up -d postgres` — migrations 001/002/003 auto-apply via `/docker-entrypoint-initdb.d` — or re-run `make test-rls`, which re-applies 001→003 idempotently against an already-running container).
2. `make test-rls` — runs `db/tests/rls_test.sql`, `db/tests/cv_ingestions_rls.test.sql`, and the new `db/tests/nullif_guc.test.sql` via `pg_prove` as `app_user`. Confirms T-122.
3. Against the same live DB, with `TEST_DATABASE_URL` (and optionally `TEST_ADMIN_DATABASE_URL`) exported:
   ```
   TEST_DATABASE_URL="postgres://app_user:app_pw@localhost:5432/careerops?sslmode=disable" \
     cd api && go test ./internal/platform/... ./internal/testsupport/... ./internal/cv/... -count=1 -v
   ```
   Confirms: `WithTenantTx`'s commit/rollback/cross-tenant-denial behavior, the `rlsdb` harness's cross-tenant denial + empty-GUC clean-deny proof, and the pre-existing `cv` ingest RLS test all pass together against the fully-migrated (001+002+003) DB.
4. `make test-all` for the full unit suite (Go + worker + web) as a final sanity check — no worker/web files were touched by this seam, so this should be a no-op confirmation.

### Remaining tasks

- [ ] T-122 — run `make test-rls` against a live DB with migration 003 applied (orchestrator)
- [ ] T-130's `make test-rls` portion — same as above
- [ ] Seam 2 (`scan`) — T-131..T-133
- [ ] Seam 3 (`tracker`) — T-134..T-136
- [ ] Seam 4 (`auth`) — T-137..T-139
- [ ] Seam 5 (`jobs`) — T-140..T-142
- [ ] Seam 6 (`evaluate`) — T-143..T-145
- [ ] Seam 7 (`companies`) — T-146..T-148
- [ ] Seam 8 (`cv` remaining 5 methods) — T-149..T-152
- [ ] Seam 9 (cross-seam final verification) — T-153..T-156

### Workload / PR boundary

- Mode: chained PR slice (`stacked-to-main` per `tasks.md`'s recommendation, pending final confirmation by the user/orchestrator per `ask-on-risk`)
- Current work unit: PR-1 — foundation (Seam 1 only, T-118..T-130)
- Boundary: starts from a clean `main`-derived branch (`feat/rls-foundation`); ends at a fully compiling, fully-passing-unit-tests state with the NULLIF migration, `WithTenantTx`, the `rlsdb` harness, dead-code removal, and the `cv` re-point all landed together (per `tasks.md`'s sequencing note: "Recommend landing the DB sub-chain and Go sub-chain in the same PR-1 to keep [the NULLIF-before-tenant-tx] invariant simple to reason about")
- Estimated review budget impact: ~210-235 hand-written lines per the Review Workload Forecast in `tasks.md` — comfortably the largest single seam but still under the 400-line budget on its own

### Status

13/13 Seam 1 tasks addressed (11 done outright by this batch; T-122 and the `make test-rls` portion of T-130 deferred to the orchestrator's live-DB pass, which this batch was explicitly instructed not to run). Ready for live-DB verification, then ready for Seam 2-8 `sdd-apply` batches (parallelizable, any order, all depend only on this seam).

---

## Seam 2 — `scan` slice: `GetScanRun` + `TriggerScan`

> Branch: `feat/rls-scan` (based on `main`, which now includes Seam 1's merged foundation)

### TDD Cycle Evidence

| Task | RED | GREEN | REFACTOR |
|------|-----|-------|----------|
| T-131/T-132 (`scan.GetScanRun` + `scan.TriggerScan` wired to `platform.WithTenantTx`) | Wrote `api/internal/scan/rls_integration_test.go` using the `rlsdb` harness BEFORE wiring the service. Ran against a live NULLIF-migrated DB (`docker compose up -d postgres`) with `TEST_DATABASE_URL` set — confirmed RED: `owner_GetScanRun_still_succeeds` failed with `sql: no rows in result set` and `TriggerScan_...` failed with `ERROR: new row violates row-level security policy for table "scan_runs" (SQLSTATE 42501)`, because `GetScanRun`/`TriggerScan` ran over the raw pool with no `app.current_user_id` GUC set — RLS denied everything, including the owner's own rows. The cross-tenant-denial subtest passed even in RED (denial was already true by accident, for the wrong reason — no GUC at all, not a properly-scoped denial) | Wired `scan.Service.GetScanRun`'s `GetScanRunByID` call and `scan.Service.TriggerScan`'s `ListEnabledWatchedCompaniesByUser` + `InsertScanRun` calls through `platform.WithTenantTx`, keeping the `queue.Enqueue` loop outside the tx (after commit) and the app-layer `scanRun.UserID != userID` check in `GetScanRun` as defense-in-depth. Re-ran the same test — all 3 subtests PASS | n/a — straight wiring, mirrors the `cv.EnqueueIngest`/`GetIngestion` precedent exactly; no refactor needed |
| T-133 (verify) | n/a | `cd api && go test ./internal/scan/... -count=1` against the live DB: 9/9 tests pass (8 pre-existing mock-based `handler_test.go` tests unmodified + 1 new `TestScanRLS_Integration` with 3 subtests). Re-ran 3x in isolation for stability — all green. Also ran `go build ./...`, `go vet ./...`, and `make test-go` (full unit suite, no `TEST_DATABASE_URL` — confirms clean skip path) — all green | n/a |

### Files changed

| File | Action | What was done |
|------|--------|----------------|
| `api/internal/scan/rls_integration_test.go` | Created | `TestScanRLS_Integration` using the `rlsdb` harness: seeds users A and B, seeds a `scan_runs` row for A via `AdminPool`, asserts B's `GetScanRun(A's runID)` is denied with `sql.ErrNoRows` (RLS denial, proven independent of the app-layer check since it is exercised purely at the DB layer through `app_user`), asserts A's own `GetScanRun` succeeds, asserts `TriggerScan` for A inserts a `scan_runs` row and enqueues at least one `scan-company` pgboss job. |
| `api/internal/scan/service.go` | Modified | `GetScanRun` and `TriggerScan` now wrap their sqlc calls in `platform.WithTenantTx(ctx, s.pool, userID, fn)`. Removed the now-unused `s.queries()` helper, `pgx/v5/stdlib` import; added `platform` import. `TriggerScan`'s `queue.Enqueue` loop stays outside the tx, after commit, capturing `companies`/`scanRun` via closure variables. `GetScanRun`'s app-layer `scanRun.UserID != userID` check is preserved as defense-in-depth (PR #8), now running against an RLS-backed result. |

### Deviations from design

None — implementation matches `design.md` §2 (`scan` row in the per-domain method-by-method table) and §1.3 exactly: both methods replace direct `s.queries()` calls with `platform.WithTenantTx`, the enqueue loop stays outside the tx, and the app-layer ownership check in `GetScanRun` is kept, not removed.

One addition beyond the literal task description: T-131's test also asserts `TriggerScan` enqueues at least one `pgboss.job` row (via `h.EnsurePgbossStandin` + a seeded enabled `watched_companies` row), not just that the `scan_runs` row is inserted — this exercises the full "outside the tx, after commit" enqueue path end-to-end rather than only the DB-write portion, closing a gap the task description's wording ("insert correctly") left implicit.

### Issues found

None. One pre-existing (not introduced by this seam) test-infra hazard noted for awareness: running `./internal/scan/...` together with `./internal/cv/...` (and other packages using the same `EnsurePgbossStandin`/`ensurePgbossStandin` `CREATE TABLE IF NOT EXISTS pgboss.job` DDL) under Go's default parallel-package test execution against the same live DB can intermittently race with `ERROR: tuple concurrently updated (SQLSTATE XX000)`. This is shared infrastructure from Seam 1 (the `rlsdb` harness and the `cv` test both have this DDL), not scan-specific, and is avoided by running packages sequentially (`-p 1`) or any single package alone — confirmed stable across 3 consecutive runs of `./internal/scan/...` alone. Not fixed in this batch (out of scope — `rlsdb` harness file belongs to Seam 1, already merged to `main`); flagging for the cross-seam verification pass (T-155) since it will exercise multiple domain packages together against one live DB.

### Workload / PR boundary

- Mode: chained PR slice (`stacked-to-main`)
- Current work unit: PR-2 — `scan` slice (Seam 2 only, T-131..T-133)
- Boundary: starts from `main` (post-Seam-1-merge) on branch `feat/rls-scan`; ends at a fully compiling, fully-passing state with `scan.GetScanRun`/`scan.TriggerScan` wired through `platform.WithTenantTx`, the new integration test passing against a live NULLIF-migrated DB, and zero changes to any other domain (`tracker`, `auth`, `jobs`, `evaluate`, `companies`, `cv` untouched, per scope)
- Estimated review budget impact: ~70 hand-written lines per the Review Workload Forecast in `tasks.md` (service ~30, integration test ~40) — well under the 400-line budget, independent of every other seam

### Status

3/3 Seam 2 tasks complete (T-131, T-132, T-133). Cumulative: 14/16 tasks across Seams 1-2 complete (T-122 and T-130's `make test-rls` portion remain deferred to the orchestrator's live pgTAP run, as in the prior batch). Ready for verify, then ready for Seam 3-8 `sdd-apply` batches (parallelizable, any order, all depend only on Seam 1).

### Remaining tasks (cumulative, as of end of Seam 2)

- [ ] T-122 — full `make test-rls` pgTAP suite against a live DB (orchestrator). Not run in this batch (out of scope — T-131..T-133 only). Partial corroboration only: this batch's `docker compose up -d postgres` spin-up confirmed via direct `psql` inspection that the `tenant_scan_runs` policy on `scan_runs` already carries the NULLIF form (`user_id = (NULLIF(current_setting('app.current_user_id', true), ''))::uuid`), consistent with migration 003 having auto-applied — but the pgTAP assertions themselves were not executed
- [ ] T-130's `make test-rls` portion — same as above, not run in this batch
- [x] Seam 2 (`scan`) — T-131..T-133
- [ ] Seam 3 (`tracker`) — T-134..T-136
- [ ] Seam 4 (`auth`) — T-137..T-139
- [ ] Seam 5 (`jobs`) — T-140..T-142
- [ ] Seam 6 (`evaluate`) — T-143..T-145
- [ ] Seam 7 (`companies`) — T-146..T-148
- [ ] Seam 8 (`cv` remaining 5 methods) — T-149..T-152
- [ ] Seam 9 (cross-seam final verification) — T-153..T-156

---

## Seam 3 — `tracker` slice: `UpdateApplication` ordering fix + `ListApplications`

> Branch: `feat/rls-tracker` (based on `main`, which now includes Seam 1's merged foundation AND Seam 2's merged `scan` slice)

### TDD Cycle Evidence

| Task | RED | GREEN | REFACTOR |
|------|-----|-------|----------|
| T-134/T-135 (`tracker.UpdateApplication` + `tracker.ListApplications` wired to `platform.WithTenantTx`, D8 post-UPDATE check dropped) | Wrote `api/internal/tracker/rls_integration_test.go` using the `rlsdb` harness BEFORE wiring the service. Seeded a `jobs` row (FK parent, `applications.job_id` is `NOT NULL UNIQUE`) + an `applications` row for user A via `AdminPool`. Ran against a live NULLIF-migrated DB (`docker compose up -d postgres`) with `TEST_DATABASE_URL` set — confirmed RED: the cross-tenant-denial subtest passed (by RLS-with-no-GUC accident, same pattern as Seam 2), but `owner_UpdateApplication_still_succeeds_...` failed with `not found` — because `UpdateApplication` ran over the raw pool (`s.queries()`) with no `app.current_user_id` GUC set, so RLS denied the owner's own UPDATE too, the UPDATE returned `sql.ErrNoRows`, and the service mapped that to `ErrNotFound` | Wired `tracker.Service.UpdateApplication`'s `UpdateApplicationStatus`/`UpdateApplicationNotes` calls (both in the SAME `platform.WithTenantTx` call when both fire — atomic per design.md §2) and `tracker.Service.ListApplications`'s `ListApplicationsByUser` call through `platform.WithTenantTx`. Deleted the now-unused `s.queries()` helper and its `pgx/v5/stdlib` import; added `platform` import. **Dropped** the post-UPDATE `if updated.UserID != userID` check per design.md D8 (tracker-specific guidance — explicitly differs from scan's keep-as-defense-in-depth pattern). Re-ran the same test — both subtests PASS | n/a — straight wiring, mirrors the `scan`/`cv` precedent; the only structural change is collapsing 3 separate UPDATE branches (status-only / notes-only / both) into one `WithTenantTx` closure with two conditional UPDATEs, which also fixed the pre-existing bug where the both-fields branch's first UPDATE's ownership check (`updated.UserID != userID`) ran on a partially-applied result before the second UPDATE — this whole hazard disappears once RLS is the only guard |
| T-136 (verify) | n/a | `cd api && go test ./internal/tracker/... -count=1` against the live DB: 10/10 tests pass (9 pre-existing mock-based `handler_test.go` tests unmodified + 1 new `TestTrackerRLS_Integration` with 2 subtests). Re-ran 3x in isolation for stability — all green. Also confirmed the test skips cleanly without `TEST_DATABASE_URL` (`go test ./internal/tracker/... -count=1 -v` with no env var set → `SKIP: TestTrackerRLS_Integration`). Ran `go build ./...`, `go vet ./...`, and `make test-go` (full unit suite across all Go packages, including `scan`/`cv`/`platform`/`testsupport/rlsdb` from prior seams) — all green | n/a |

### Files changed

| File | Action | What was done |
|------|--------|----------------|
| `api/internal/tracker/rls_integration_test.go` | Created | `TestTrackerRLS_Integration` using the `rlsdb` harness: seeds users A and B, seeds a `jobs` row + an `applications` row for A via `AdminPool` (jobs.url carries a UUID suffix so the test is safely re-runnable against a DB that retains prior fixtures, avoiding a `jobs_user_id_url_key` collision), asserts B's `UpdateApplication(A's appID, ...)` returns `tracker.ErrNotFound` and A's row is verified unchanged (`status` still `'Evaluated'`) via `AdminPool` after B's attempt, asserts A's own `UpdateApplication` succeeds and is visible on a subsequent `AdminPool` read (`status` now `'Applied'`). |
| `api/internal/tracker/service.go` | Modified | `ListApplications` and `UpdateApplication` now wrap their sqlc calls in `platform.WithTenantTx(ctx, s.pool, userID, fn)`. Removed the now-unused `s.queries()` helper and `pgx/v5/stdlib` import; added `platform` import. `UpdateApplication` collapsed its 3 separate update branches into one `WithTenantTx` closure containing up to 2 conditional UPDATEs (status, then notes) sharing one tx — atomic when both fire. The post-UPDATE `if updated.UserID != userID` check is **removed** (D8) — `sql.ErrNoRows` from the UPDATE itself (RLS `USING` excludes a non-owner's row from the target scan) is now mapped directly to `tracker.ErrNotFound` after `WithTenantTx` returns. |

### Deviations from design

None — implementation matches `design.md` §2 (`tracker` row in the per-domain method-by-method table) and D8 exactly: both UPDATEs share one tenant tx when both status and notes are provided, and the post-UPDATE ownership check is dropped (not kept as defense-in-depth, unlike scan's `GetScanRun` — design.md is explicit that tracker's D8 guidance differs from scan's pattern, and this was independently verified against design.md before assuming).

One incidental improvement beyond the literal task description: the pre-existing both-fields code path had a structural bug (latent, not a regression introduced by this change, but only fully eliminated by this rewrite) — the first UPDATE's `updated.UserID != userID` check ran on `UpdateApplicationStatus`'s result and could short-circuit with `ErrNotFound` even after that first UPDATE had already mutated the row, leaving `notes` unset and `status` already changed for a hypothetical caller who passed someone else's `appID` purely as a sequencing artifact (impossible in this app since `appID` ownership was never separately validated before either UPDATE ran). Collapsing both UPDATEs into one `WithTenantTx` closure with RLS as the single gate removes this ambiguity entirely: either both UPDATEs commit (owner) or neither does (RLS denial → full rollback, since `WithTenantTx`'s `fn` returning a non-nil error rolls back the whole tx).

### Issues found

None new. Re-confirmed the same pre-existing (Seam-1-introduced, not tracker-specific) `EnsurePgbossStandin` DDL race noted in Seam 2's progress is irrelevant here — `tracker`'s integration test does not call `EnsurePgbossStandin` (no pgboss enqueue in this domain), so it is not exposed to that hazard at all.

### Workload / PR boundary

- Mode: chained PR slice (`stacked-to-main`)
- Current work unit: PR-3 — `tracker` slice (Seam 3 only, T-134..T-136)
- Boundary: starts from `main` (post-Seam-1-and-Seam-2-merge) on branch `feat/rls-tracker`; ends at a fully compiling, fully-passing state with `tracker.UpdateApplication`/`tracker.ListApplications` wired through `platform.WithTenantTx`, the D8 post-UPDATE check dropped, the new integration test passing against a live NULLIF-migrated DB, and zero changes to any other domain (`scan`, `auth`, `jobs`, `evaluate`, `companies`, `cv` untouched, per scope)
- Estimated review budget impact: ~75 hand-written lines per the Review Workload Forecast in `tasks.md` (service ~25, integration test ~50) — well under the 400-line budget, independent of every other seam

### Status

3/3 Seam 3 tasks complete (T-134, T-135, T-136). Cumulative: 17/19 tasks across Seams 1-3 complete (T-122 and T-130's `make test-rls` portion remain deferred to the orchestrator's live pgTAP run, as in prior batches). Ready for verify, then ready for Seam 4-8 `sdd-apply` batches (parallelizable, any order, all depend only on Seam 1).

---

## Seam 4 — `auth` slice (T-137..T-139, complete, this batch, branch `feat/rls-auth` based on `main` post-Seam-1/2/3)

### Files changed

| File | Action | What was done |
|------|--------|----------------|
| `api/internal/auth/rls_integration_test.go` | Created | `TestAuthRLS_Integration` using the `rlsdb` harness: seeds users A and B, asserts B's `auth.GetUserByID(ctx, pool, callerUserID=B, userID=A)` returns `auth.ErrNotFound` (RLS `USING` denial under B's tenant tx), asserts A's own `auth.GetUserByID(ctx, pool, callerUserID=A, userID=A)` succeeds and returns A's row. |
| `api/internal/auth/service.go` | Modified | `GetUserByID`'s signature changed from `(ctx, pool, userID)` to `(ctx, pool, callerUserID, userID)` — added `callerUserID` to scope the tenant tx (D6, minimal-diff option (b) from design §2: kept the existing query semantics but switched from a hand-rolled raw `pool.QueryRow` to the already-generated sqlc `q.GetUserByID(ctx, userID)` call, run inside `platform.WithTenantTx(ctx, pool, callerUserID, fn)` — no new sqlc query needed, `GetUserByID` already existed in `internal/db/users.sql.go` from a prior sqlc generation but had no caller). Added package-level `auth.ErrNotFound = errors.New("not found")` (auth had no existing not-found sentinel, unlike `cv`/`tracker`/`scan`). Maps `sql.ErrNoRows` (sqlc's `database/sql`-backed `Queries` surfaces this, not `pgx.ErrNoRows`, since `WithTenantTx` bridges the pgxpool via `stdlib.OpenDBFromPool`) to `ErrNotFound`. `auth.UpsertUser` and the underlying `auth_upsert_user` SQL function are **byte-identical, untouched** — verified via `git diff` showing zero lines changed in that function (proof by omission, Req 6). |

### Deviations from design

One: design §2's table for `auth`/`GetUserByID` says "the existing raw `SELECT` run via `tx`", and §2's gap note offers (a) add a sqlc query or (b) keep the raw SELECT wrapped in the tx as the smaller diff. On inspection, a sqlc `GetUserByID(ctx, id uuid.UUID) (User, error)` query method **already existed** in the generated `internal/db/users.sql.go` (likely generated for future use but never called) — so neither (a) nor literal (b) applied verbatim; the actual minimal-diff path was to call the pre-existing sqlc method instead of either hand-writing raw SQL inside the tx or running `sqlc generate` to add a new query. This is strictly smaller than both options in the design and changes no SQL files. Functionally identical output to the original 7-field manual `Scan` (verified — `db.User` struct fields match 1:1, `sql.NullString`/`json.RawMessage` handling is internal to the sqlc-generated scan, so the manual `cvMarkdown`/`profileJSON` post-processing in the old code became dead code and was removed).

Second, minor: `GetUserByID`'s signature gained a second `uuid.UUID` parameter (`callerUserID`) ahead of the original `userID`, since the function had no `userID`-vs-caller distinction before (it took only one ID and used it both as the row to fetch and implicitly as "trusted from JWT"). This is consistent with D6's framing ("asked to look up A's user ID" while "scoped to B") and was the only way to express the cross-tenant scenario without inventing a second function. There is currently no production caller of `GetUserByID` (`rg` found zero call sites outside its own definition and the new test) — the `Refresh` handler re-issues tokens from JWT claims without re-fetching the user row, so this signature change has zero blast radius on existing behavior today, satisfying Req 5 vacuously for this function while still proving the RLS contract for whenever a caller is wired in.

### Issues found

None. Build (`go build ./...`) and vet (`go vet ./...`) clean. No import cycle introduced by `auth` importing `platform` (one-way edge, same as `cv`/`scan`/`tracker` already established in Seam 1-3).

### Workload / PR boundary

- Mode: chained PR slice (`stacked-to-main`)
- Current work unit: PR-4 — `auth` slice (Seam 4 only, T-137..T-139)
- Boundary: starts from `main` (post-Seam-1/2/3 merge) on branch `feat/rls-auth`; ends at a fully compiling, fully-passing state with `auth.GetUserByID` wired through `platform.WithTenantTx`, the new integration test passing against a live NULLIF-migrated DB, `auth.UpsertUser`/`auth_upsert_user` verified untouched, and zero changes to any other domain (`scan`, `tracker`, `jobs`, `evaluate`, `companies`, `cv` untouched, per scope)
- Estimated review budget impact: ~75 hand-written lines (service.go net diff ~32 lines per `git diff --stat`, integration test ~50 lines) — well under the 400-line budget

### Test results

`cd api && go test ./internal/auth/... -count=1` (with `TEST_DATABASE_URL` set against the live docker-compose `postgres` service, migrated through `003_rls_nullif.sql`): **15/15 test functions pass** — 14 pre-existing tests (JWT issue/verify round-trips, `TestGoogleCallbackCreatesUser` x3 subtests, `TestRefreshTokenRotation`, `TestRefreshTokenRotation_MissingCookie`, `TestExpiredAccessToken401`, `TestExpiredAccessToken_ResponseContainsError`, `TestTokensAreDistinct`, `TestIssueAccessToken_DifferentPlans` x3 subtests) unmodified and green, plus the new `TestAuthRLS_Integration` (2 subtests: cross-tenant denial, owner success) green. Without `TEST_DATABASE_URL` set, the integration test skips cleanly (`t.Skip`) and the rest of the suite (`make test-go`, full repo) stays green across all packages (`auth`, `companies`, `cv`, `evaluate`, `jobs`, `middleware`, `platform`, `scan`, `testsupport/rlsdb`, `tracker`, `ws`).

RED confirmed before T-138: writing the test first against the old 3-arg `GetUserByID(ctx, pool, userID)` signature produced a compile-time RED (`too many arguments in call to auth.GetUserByID`), proving the test exercises behavior that did not yet exist, before the signature/impl change in T-138 made it pass.

### Status

3/3 Seam 4 tasks complete (T-137, T-138, T-139). Cumulative: 20/22 tasks across Seams 1-4 complete (T-122 and T-130's `make test-rls` pgTAP portions remain deferred to the orchestrator's live pgTAP run, as in prior batches). Ready for verify, then ready for Seam 5-8 `sdd-apply` batches (parallelizable, any order, all depend only on Seam 1).

### Remaining tasks (cumulative, as of end of Seam 4)

- [ ] T-122 — full `make test-rls` pgTAP suite against a live DB (orchestrator). Still not run (deferred since Seam 1/2; out of scope for Seam 3/4 as well)
- [ ] T-130's `make test-rls` portion — same as above, not run in this batch
- [x] Seam 2 (`scan`) — T-131..T-133
- [x] Seam 3 (`tracker`) — T-134..T-136
- [x] Seam 4 (`auth`) — T-137..T-139
- [ ] Seam 5 (`jobs`) — T-140..T-142
- [ ] Seam 6 (`evaluate`) — T-143..T-145
- [ ] Seam 7 (`companies`) — T-146..T-148
- [ ] Seam 8 (`cv` remaining 5 methods) — T-149..T-152
- [ ] Seam 9 (cross-seam final verification) — T-153..T-156

---

## Seam 5 — `jobs` slice: `AddManual` / `List` / `GetByID` + repo-from-tx (T-140..T-142, complete, this batch, branch `feat/rls-jobs` based on `main` post-Seam-1/2/3/4)

### TDD Cycle Evidence

| Task | RED | GREEN | REFACTOR |
|------|-----|-------|----------|
| T-140/T-141 (`jobs.Service.AddManual`/`List`/`GetByID` wired to `platform.WithTenantTx` + `newRepoFromQueries`) | Wrote `api/internal/jobs/rls_integration_test.go` using the `rlsdb` harness BEFORE wiring the service. Ran against a live NULLIF-migrated DB (`docker compose up -d postgres`) with `TEST_DATABASE_URL` set — confirmed RED: the owner's own `AddManual` call failed with `ERROR: new row violates row-level security policy for table "jobs" (SQLSTATE 42501)`, because `s.repo()` built a `Repo` over the raw pool with no `app.current_user_id` GUC set, so the `WITH CHECK` clause on the INSERT denied even the owner's own write | Added `newRepoFromQueries(q *db.Queries) *Repo` to `jobs/repo.go` (replacing `NewRepo(pool)` and `newRepoFromSQL(sqlDB)` — both deleted, confirmed zero other callers via grep before deletion). Wired `AddManual`/`List`/`GetByID` in `jobs/service.go` to build the `Repo` from inside `platform.WithTenantTx(ctx, s.pool, userID, fn)`'s closure instead of `s.repo()` over the raw pool; deleted the now-dead `s.repo()` method and its `pgx/v5/stdlib` import, added `platform` import. `GetByID` keeps its post-lookup `if job.UserID != userID` check as defense-in-depth (consistent with `scan.GetScanRun`'s kept pattern, not `tracker`'s D8 drop-it pattern — `jobs.GetByID` is a pure read with no WITH-CHECK mutation risk, so the design's per-domain table does not call for removing it). Re-ran the same test — all 3 subtests PASS | n/a — straight wiring, mirrors the `scan`/`auth` precedent (build `*db.Queries`-backed `Repo` inside the tx closure, capture the result via an outer variable, return it after `WithTenantTx` returns) |
| T-142 (verify) | n/a | `cd api && go test ./internal/jobs/... -count=1` against the live DB: 21/21 test functions pass (20 pre-existing — `TestDetectPlatform` table-driven + 10 mock-based `handler_test.go` tests + the pre-existing app-layer cross-tenant tests `TestUserBCannotReadUserAJob`/`TestUserBCannotListUserAJobs`/`TestCrossTenantJobAccess_DirectUUID`/`TestAuthenticatedUserCanReadOwnJob` — all unmodified, plus the new `TestJobsRLS_Integration` with 3 subtests). Confirmed clean skip without `TEST_DATABASE_URL` (`SKIP: TestJobsRLS_Integration`). Ran `go build ./...`, `go vet ./...` (both clean), and the full repo `go test ./... -count=1` (no `TEST_DATABASE_URL`) — all 13 testable packages green | n/a |

### Files changed

| File | Action | What was done |
|------|--------|----------------|
| `api/internal/jobs/rls_integration_test.go` | Created | `TestJobsRLS_Integration` using the `rlsdb` harness: seeds users A and B, A adds a job via `svc.AddManual`, asserts B's `GetByID(jobA.ID)` returns `jobs.ErrNotFound`, asserts B's `List` never includes A's job, asserts A's own `GetByID`/`List`/`AddManual` (second job) all still succeed and A's `List` returns exactly A's own job(s). |
| `api/internal/jobs/repo.go` | Modified | Deleted `NewRepo(pool *pgxpool.Pool) *Repo` (zero callers anywhere, grep-confirmed) and `newRepoFromSQL(sqlDB *sql.DB) *Repo` (exactly one caller — the now-deleted `s.repo()` — grep-confirmed). Added `newRepoFromQueries(q *db.Queries) *Repo` — the sole constructor now, taking an already tenant-scoped `*db.Queries` produced inside `platform.WithTenantTx`'s closure. Dropped the now-unused `database/sql`, `pgx/v5/stdlib`, `pgx/v5/pgxpool` imports. |
| `api/internal/jobs/service.go` | Modified | Deleted the `s.repo()` method (built a raw-pool `Repo` via `stdlib.OpenDBFromPool` — no GUC scoping). `AddManual`, `List`, `GetByID` now each wrap their repo call in `platform.WithTenantTx(ctx, s.pool, userID, fn)`, building `newRepoFromQueries(q)` inside the closure and capturing the result via an outer variable. Dropped the now-unused `pgx/v5/stdlib` import; added `platform` import. `GetByID`'s post-lookup ownership recheck is kept as defense-in-depth (unchanged from before, consistent with `scan`). |

### Deviations from design

None — implementation matches `design.md` §1.3 ("`jobs` (Repo indirection)" bullet — add `newRepoFromQueries`, build the `Repo` from the tx, remove `NewRepo`/`newRepoFromSQL` if unused) and §2's `jobs` rows in the per-domain table (`AddManual`/`List`/`GetByID` all move inside the tenant tx; `GetByID`'s ownership recheck explicitly noted as "recommend keep one line as defense-in-depth, consistent with scan") exactly.

Grep evidence for the dead-constructor removal, run before deleting:
```
rg -n "jobs\.NewRepo|newRepoFromSQL|jobs\.Repo\{" --type go .
```
Result: `newRepoFromSQL` had exactly one call site (`jobs/service.go`'s `s.repo()`, itself removed in this same change); `jobs.NewRepo`/`NewRepo(pool)` had zero call sites anywhere in the tree (not even in tests). Both were safe to delete outright — no other caller remained.

### Issues found

None. Confirmed the same RED pattern as Seam 2/3 (Seam 5's RED was the owner's own write being denied by RLS over the unscoped raw pool, not a cross-tenant leak being silently allowed) — this is expected and consistent: every prior service slice's pre-wiring state denies everything (including the owner) once a live NULLIF-migrated DB is in play, because `WITH CHECK`/`USING` require a properly-set GUC that the raw pool never sets. The cross-tenant assertions in this test would have passed even in RED, for the "wrong" reason (no GUC at all rather than a correctly-scoped denial) — same caveat already logged in Seam 2 and Seam 3's progress notes.

### Workload / PR boundary

- Mode: chained PR slice (`stacked-to-main`)
- Current work unit: PR-5 — `jobs` slice (Seam 5 only, T-140..T-142)
- Boundary: starts from `main` (post-Seam-1/2/3/4 merge) on branch `feat/rls-jobs`; ends at a fully compiling, fully-passing state with `jobs.AddManual`/`List`/`GetByID` wired through `platform.WithTenantTx` + `newRepoFromQueries`, the dead raw-pool `Repo` constructors removed, the new integration test passing against a live NULLIF-migrated DB, and zero changes to any other domain (`auth`, `scan`, `tracker`, `evaluate`, `companies`, `cv` untouched, per scope)
- Estimated review budget impact: ~90 hand-written lines per the Review Workload Forecast in `tasks.md` (service+repo ~40, integration test ~50) — well under the 400-line budget, independent of every other seam

### Test results

`cd api && go test ./internal/jobs/... -count=1 -v` (with `TEST_DATABASE_URL` set against the live docker-compose `postgres` service, migrated through `003_rls_nullif.sql`): **21/21 test functions pass** — 10 `TestDetectPlatform` table-driven subtests, 10 pre-existing mock-based `handler_test.go` tests (`TestList_*`, `TestCreate_*`, `TestGetByID_*`, `TestUserBCannotReadUserAJob`, `TestUserBCannotListUserAJobs`, `TestCrossTenantJobAccess_DirectUUID`, `TestAuthenticatedUserCanReadOwnJob`, `TestUnauthenticatedRequestReturns401_GetByID`, `TestUnauthenticatedRequestReturns401_List`) unmodified and green, plus the new `TestJobsRLS_Integration` (3 subtests: non-owner `GetByID` denial, non-owner `List` exclusion, owner `GetByID`/`List`/`AddManual` success) green. Without `TEST_DATABASE_URL` set, the integration test skips cleanly (`SKIP: TestJobsRLS_Integration`) and the rest of the suite (full repo `go test ./... -count=1`) stays green across all 13 testable packages (`auth`, `companies`, `cv`, `evaluate`, `jobs`, `middleware`, `platform`, `scan`, `testsupport/rlsdb`, `tracker`, `ws`).

RED confirmed before T-141: running the test first against the OLD `s.repo()`-over-raw-pool implementation produced a runtime RED against the live DB — `upsert job: ERROR: new row violates row-level security policy for table "jobs" (SQLSTATE 42501)` on the owner's own `AddManual` call, proving the pre-wiring code does not engage RLS correctly (it denies everyone, including the owner, rather than scoping by tenant) — before the `WithTenantTx` wiring in T-141 made the test pass.

### Status

3/3 Seam 5 tasks complete (T-140, T-141, T-142). Cumulative: 23/25 tasks across Seams 1-5 complete (T-122 and T-130's `make test-rls` pgTAP portions remain deferred to the orchestrator's live pgTAP run, as in prior batches). Ready for verify, then ready for Seam 6-8 `sdd-apply` batches (parallelizable, any order, all depend only on Seam 1).

### Remaining tasks (cumulative, as of end of Seam 5)

- [ ] T-122 — full `make test-rls` pgTAP suite against a live DB (orchestrator). Still not run (deferred since Seam 1/2; out of scope for Seam 3/4/5 as well)
- [ ] T-130's `make test-rls` portion — same as above, not run in this batch
- [x] Seam 2 (`scan`) — T-131..T-133
- [x] Seam 3 (`tracker`) — T-134..T-136
- [x] Seam 4 (`auth`) — T-137..T-139
- [x] Seam 5 (`jobs`) — T-140..T-142
- [ ] Seam 6 (`evaluate`) — T-143..T-145
- [ ] Seam 7 (`companies`) — T-146..T-148
- [ ] Seam 8 (`cv` remaining 5 methods) — T-149..T-152
- [ ] Seam 9 (cross-seam final verification) — T-153..T-156
