# Apply Progress — `rls-tenancy-wiring`

> Phase: apply · Status: Seam 1 (foundation) complete, Seams 2-8 not started
> Branch: `feat/rls-foundation` (created fresh from `main`)
> Mode: Strict TDD (test runner: `make test-all` / `cd api && go test ./... -count=1`; `make test-rls` for pgTAP)
> Batch: 1 of N (first apply for this change)

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
