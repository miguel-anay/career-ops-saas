# Design — `rls-tenancy-wiring`

> Phase: design · Status: complete · Artifact store: openspec
> Inputs: `proposal.md` (approved approach = generalize `cv.withTenant`), `explore.md`; engram mirrors `sdd/rls-tenancy-wiring/proposal` (#251), `sdd/rls-tenancy-wiring/explore` (#250).
> Skills applied: `cognitive-doc-design`, `go-testing`.

## The shape in one picture

```
HTTP request (JWT → userID in ctx via middleware.GetUserID)
   │
   ▼
domain Handler ──► domain Service.Method(ctx, userID, …)
   │                  │
   │                  ├─ tenant-scoped DB work ─────────────────────────────┐
   │                  │     platform.WithTenantTx(ctx, pool, userID, fn):    │
   │                  │        BEGIN                                         │
   │                  │        SELECT set_config('app.current_user_id',$1,true)
   │                  │        fn(db.New(tx))  ── all sqlc calls here ──►  RLS engaged
   │                  │        COMMIT (rollback on err)                      │
   │                  │  ◄──────────────────────────────────────────────────┘
   │                  │
   │                  └─ NON-tenant / external I/O OUTSIDE the tx:
   │                        queue.Enqueue(ctx, s.pool, …)   (pgboss schema, no RLS)
   │                        r2.SignedDownloadURL(...)       (network, no pooled conn held)
   ▼
HTTP response
```

Every tenant table now sees a real `app.current_user_id`. The DB is the boundary: a
non-owner read returns zero rows (→ `ErrNotFound`/404), a non-owner write is rejected by
`WITH CHECK`. The 9 RLS policies are hardened so an **empty-string** GUC degrades to clean
denial instead of `22P02`.

## Decisions at a glance

| # | Decision | Rationale |
|---|----------|-----------|
| D1 | **Promote the tenant helper to `platform.WithTenantTx(ctx, pool, userID, fn func(*db.Queries) error) error`** — a package-level function, not a per-service method. | The exact body already exists and is proven (`cv.withTenant`). Lifting it into `platform` (which already owns `NewPool` + the dead `WithTenant`) dedupes it across 7 domains and gives one audited tenancy mechanic for the whole API. A per-service copy would re-introduce 7× the GUC-handling surface to review. |
| D2 | **Replace the dead `platform.WithTenant(... fn func(*pgx.Conn))`** (acquire-conn, `SET LOCAL` via string-fmt, zero call sites) with the new tx-based form. | The existing one is unused, uses `fmt.Sprintf` interpolation (injection-shaped, even if `userID` is a UUID), and returns a raw `*pgx.Conn` not a `*db.Queries`. Keeping both invites the wrong one being copied. Remove it in the same slice that introduces `WithTenantTx`. |
| D3 | **Migrate the merged `cv.withTenant` to call `platform.WithTenantTx`** (keep a 1-line `cv.withTenant` shim or inline the call). | Dedup. `cv` is the precedent; it must not stay a second copy of the pattern. Recommendation: delete `cv.withTenant`, call `platform.WithTenantTx(ctx, s.pool, userID, fn)` directly at its 2 call sites — the method added nothing but the pool capture. |
| D4 | **Each service keeps its `*pgxpool.Pool` field**; methods switch from `s.queries()` / `s.repo()` to wrapping their sqlc calls in `platform.WithTenantTx`. `queue.Enqueue` stays on `s.pool`. | Smallest blast radius (proposal Approach 1). No change to `db.Queries`/`DBTX`, no pgx-native rewrite, no context-plumbed per-request tx. The tx boundary stays at method level, exactly where `cv` already put it. |
| D5 | **The NULLIF policy hardening ships FIRST as its own atomic migration** `db/migrations/003_rls_nullif.sql`, mirrored into `db/rls.sql`. | Required for correctness under connection pooling regardless of wiring order: a partially-wired pool (tenant tx ran, then a not-yet-migrated raw query reuses the same physical connection) is exactly when empty-string `''::uuid` → `22P02` fires. Landing it first makes every later service slice merge onto a DB where empty GUC = deny, not 500. |
| D6 | **`auth.GetUserByID` switches from a hand-rolled `pool.QueryRow` to `WithTenantTx` + a sqlc `GetUserByID` query** (or wraps the existing raw query in the tx). | It is the refresh-token read with no app filter and no `SECURITY DEFINER` — 100% RLS-reliant and likely broken today. It is outside the `/api` prefix conventions and easy to miss; it gets explicit treatment and its own integration test. |
| D7 | **A shared DB-gated test harness** lives in a new internal package `api/internal/testsupport/rlsdb` (two pools, seed-via-`auth_upsert_user`, pgboss stand-in, skip-without-`TEST_DATABASE_URL`), generalized from `cv/ingest_integration_test.go`. Per-domain `*_integration_test.go` files consume it. | One audited harness vs. 7 copies of the 90-line two-pool boilerplate. Per-domain test files stay short and assertion-focused. The existing `cv` test can optionally be refactored onto it later (NOT required by this change — leave `cv` green as-is). |
| D8 | **`tracker.UpdateApplication`: move the UPDATE(s) inside `WithTenantTx`; drop the post-UPDATE `if updated.UserID != userID` checks.** | Once the UPDATE runs under the tenant GUC, RLS `USING` makes a cross-tenant row invisible (UPDATE affects 0 rows → `sql.ErrNoRows` → `ErrNotFound`) and `WITH CHECK` gates any attempt to write a foreign `user_id`. The app-layer check becomes dead code; keeping it is redundant-but-safe, but removing it is clearer and removes the false impression it was ever the guard. Keep it ONLY if a reviewer prefers belt-and-suspenders — recommend drop. |
| D9 | **The 22P02-vs-deny live behavior is verified by a one-task spike BEFORE the migration slice**, not assumed. | The whole prerequisite rests on the empirical failure mode (no DB was available during explore). The spike (stand up the careerops container, hit an unset/empty GUC, confirm `22P02`, apply NULLIF, confirm clean deny) de-risks D5 and the ordering. It is the first task `sdd-tasks` should emit. |

---

## 1. The shared tenant helper — `api/internal/platform/postgres.go`

### 1.1 New function (replaces the dead `WithTenant`)

```go
// WithTenantTx runs fn inside a transaction with app.current_user_id set via
// set_config(..., true) (transaction-local), so RLS policies on tenant tables
// are enforced for the duration of fn. Commits if fn returns nil, rolls back
// otherwise. The pgxpool is wrapped via stdlib so the database/sql-based
// sqlc *db.Queries can run on the tx.
//
// Use this for EVERY tenant-table access. Do NOT use it for pgboss.* writes
// (queue.Enqueue) — that schema has no RLS policy and must stay on the raw pool.
func WithTenantTx(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, fn func(q *db.Queries) error) error {
    sqlDB := stdlib.OpenDBFromPool(pool)
    tx, err := sqlDB.BeginTx(ctx, nil)
    if err != nil {
        return fmt.Errorf("begin tenant tx: %w", err)
    }
    defer func() { _ = tx.Rollback() }()

    if _, err := tx.ExecContext(ctx, "SELECT set_config('app.current_user_id', $1, true)", userID.String()); err != nil {
        return fmt.Errorf("set tenant user: %w", err)
    }
    if err := fn(db.New(tx)); err != nil {
        return err
    }
    return tx.Commit()
}
```

> **Import note:** `platform` importing `internal/db` is a new edge. Verify there is no import cycle — `db` is sqlc-generated and imports nothing from `platform`, so the edge is one-way and safe. (`cv.withTenant` already imports `db`; we are moving that exact dependency up one level.)

> **Body is byte-for-byte the proven `cv.withTenant`** (lines 53-70 of `cv/service.go`), only the receiver becomes a `pool` parameter. Zero behavioral change to the already-tested path.

### 1.2 Delete the old `WithTenant` (D2)

Remove the existing `func WithTenant(ctx, pool, userID string, fn func(*pgx.Conn) error)` (lines 24-40). Drop the now-unused `github.com/jackc/pgx/v5` import if nothing else in the file uses it. The stale reference comment in `middleware/tenant.go` is addressed in §6.

### 1.3 Adoption shape per service

Two flavors, by current indirection:

- **Direct `queries()` users** (`scan`, `evaluate`, `companies`, `tracker`, remaining `cv`): replace `q := s.queries()` + calls with `platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error { … })`. Delete the now-dead `queries()` helper from each (or leave it only if still used by a non-tenant path — none are; remove).
- **`jobs` (Repo indirection)**: `jobs.Service` builds a `*Repo` via `s.repo()` (`newRepoFromSQL(stdlib.OpenDBFromPool(pool))`). Keep the `Repo` type but construct it from the tx: inside `WithTenantTx`, build `&Repo{q: q}`. Simplest: add `newRepoFromQueries(q *db.Queries) *Repo` and call the repo methods inside the `fn`. `jobs/repo.go`'s `NewRepo(pool)` and `newRepoFromSQL` become unused → remove if no other caller (verify with grep in apply).

---

## 2. Per-domain method-by-method plan

Each cell below is "what the `fn` body does inside `WithTenantTx`". The handler signatures and `Servicer` interfaces are **unchanged** — this is behavior-preserving for owners.

| Service | Method | Inside the tenant tx | Outside the tx | Notes |
|---|---|---|---|---|
| `auth` | `GetUserByID` | the `users` lookup (sqlc `GetUserByID` or the existing raw `SELECT` run via `tx`) | — | D6. Refresh-token path. Maps `sql.ErrNoRows` → a not-found error. Currently a free function `GetUserByID(ctx, pool, …)` — keep the signature, wrap the body. `UpsertUser` is **untouched** (SECURITY DEFINER, runs pre-tenant). |
| `jobs` | `AddManual` | `r.UpsertByURL(...)` (INSERT/UPSERT, gated by `WITH CHECK`) | URL parse + `detectPlatform` (pure, can stay before/after) | Repo built from tx (§1.3). |
| `jobs` | `List` | `r.ListByUser(...)` | pagination math (pure) | App `WHERE user_id=$1` stays; now redundant with RLS. |
| `jobs` | `GetByID` | `r.GetByID(...)` | — | Drop or keep the `job.UserID != userID` recheck (redundant-but-safe; recommend keep one line as defense-in-depth, consistent with scan). `sql.ErrNoRows` → `ErrNotFound`. |
| `scan` | `TriggerScan` | `ListEnabledWatchedCompaniesByUser` + `InsertScanRun` (both gated) | the `queue.Enqueue` loop (one per company) — **must stay outside**, pgboss schema | Capture `scanRun.ID` and the companies slice out of the `fn` closure, then enqueue. |
| `scan` | `GetScanRun` | `GetScanRunByID(scanRunID)` | — | **Highest-priority slice.** Non-owner row is invisible under RLS → `sql.ErrNoRows` → return `ErrNotFound`/`sql.ErrNoRows` (current handler maps to 404). The **already-merged app-layer `if scanRun.UserID != userID` check stays as defense-in-depth** (do NOT redesign the IDOR fix — PR #8). Now RLS is the primary gate, app-check the backstop. |
| `evaluate` | `EnqueueEvaluation` | `GetJobByID` (ownership) + `GetUsageByUserMonth` | the `queue.Enqueue` | Job lookup + usage read in ONE tx; enqueue after commit (mirror `cv.EnqueueIngest`). |
| `evaluate` | `GetReport` | `GetJobByID` → `GetApplicationByJobID` → `GetReportByApplicationID` (chain) | — | All three reads in one tx so RLS is consistent across the chain. `ErrNotFound` on any miss. |
| `companies` | `List` | `ListWatchedCompaniesByUser` | — | |
| `companies` | `Add` | `InsertWatchedCompany` (WITH CHECK) | `DetectProvider` (pure) | |
| `companies` | `Remove` | `GetWatchedCompanyByID` + `DeleteWatchedCompany` in ONE tx | — | `DeleteWatchedCompany` has **no `WHERE user_id`** — RLS is the ONLY mutation guard. Both statements must share the tenant tx. Keep the pre-delete ownership check as backstop. |
| `tracker` | `ListApplications` | `ListApplicationsByUser` | pagination math | |
| `tracker` | `UpdateApplication` | the UPDATE(s) — `UpdateApplicationStatus` and/or `UpdateApplicationNotes` | status validation (pure, before the tx) | **D8 ordering fix.** The mutating UPDATE now runs under the GUC; `WITH CHECK` gates cross-tenant writes; 0-rows → `ErrNotFound`. **Drop** the post-UPDATE `if updated.UserID != userID` checks (recommend) — they ran AFTER the unscoped UPDATE and were dead-for-security. If both status+notes: both UPDATEs in the SAME tx (atomic). |
| `cv` | `EnqueuePDFGeneration` | `GetApplicationByJobID` + `GetReportByApplicationID` + `GetMasterCVByUser` | the `queue.Enqueue` | Three reads in one tx; enqueue after commit. |
| `cv` | `GetDownloadURL` | `GetApplicationByJobID` ONLY | **`r2.SignedDownloadURL(...)`** — MUST be outside the tx | **Risk-driven split.** Read the application row inside the tenant tx, capture `pdf_path`, **commit/exit the tx, THEN** call R2. Never hold a pooled connection across the R2 network round-trip (see §7). |
| `cv` | `ListCVs` | `ListCVsByUser` | — | |
| `cv` | `CreateCV` | `InsertCV` (WITH CHECK) | input validation (pure) | |
| `cv` | `SetMasterCV` | `SetMasterCV` | — | `SetMasterCVParams` already carries `UserID`; RLS now also gates it. |
| `cv` | `EnqueueIngest`, `GetIngestion` | already correct via `cv.withTenant` | enqueue already outside | Only change: re-point at `platform.WithTenantTx` (D3). No behavior change. |

**Generated-query gap to confirm in apply:** `auth.GetUserByID` is currently raw SQL, not sqlc. Either (a) add a `GetUserByID` sqlc query to `db/queries/users.sql` + regenerate, or (b) keep the raw `SELECT` but run it via `tx.QueryRowContext` inside `WithTenantTx` (no regen). Recommend (a) for consistency, but (b) is a smaller diff — `sdd-tasks` picks per review budget.

---

## 3. The NULLIF migration — `db/migrations/003_rls_nullif.sql`

Next sequential number after `002_ingest_cv.sql`. Hardens all 9 policies. PG16 cannot `ALTER POLICY … USING` to change the expression in-place cleanly for both `USING` and `WITH CHECK` in one shot without re-stating them, so **DROP + CREATE each policy** (idempotent, explicit, matches how `001`/`002` define them).

```sql
-- 003_rls_nullif.sql
-- Harden every tenant policy so an EMPTY-STRING app.current_user_id (left on a
-- pooled physical connection after a prior tenant tx ended) degrades to a clean
-- DENY instead of casting ''::uuid -> 22P02 (a 500). NULL (GUC never set) already
-- denied; this extends that to '' as well.
--
-- No change to ENABLE / FORCE ROW LEVEL SECURITY flags — they stay as in 001/002.

DROP POLICY tenant_users ON users;
CREATE POLICY tenant_users ON users
  USING      (id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);

DROP POLICY tenant_watched_companies ON watched_companies;
CREATE POLICY tenant_watched_companies ON watched_companies
  USING      (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);

-- … repeat verbatim for: jobs, applications, reports, cvs, scan_runs, usage, cv_ingestions
```

(9 policies total: `tenant_users` keys on `id`; the other 8 key on `user_id`.)

### 3.1 Mirror into the canonical files

- **`db/rls.sql`** — replace the bare `current_setting('app.current_user_id', true)::uuid` in all 9 `CREATE POLICY` blocks with the `NULLIF(..., '')::uuid` form. This file is the from-scratch source of truth; a fresh boot must match a migrated DB.
- **`db/schema.sql`** — **no change needed.** Verified: `schema.sql` contains no `CREATE POLICY` / `FORCE ROW LEVEL` lines (RLS lives only in `rls.sql` + the migrations). Do NOT add policy DDL there.
- **`db/migrations/001_initial.sql` / `002_ingest_cv.sql`** — leave as historical record; migration 003 supersedes their policy bodies at runtime. Do not rewrite past migrations.

### 3.2 Flags

ENABLE + FORCE ROW LEVEL SECURITY are unchanged — DROP/CREATE POLICY does not touch table-level RLS enablement. `make test-rls` (pgTAP) must stay green after 003: the policies still enforce isolation; the only new behavior is `'' → deny` instead of `'' → error`.

**Changed-line budget — DB:** ~60 lines (migration ~40 for 9 DROP/CREATE pairs, `rls.sql` ~18 edits).

---

## 4. Test design

### 4.1 Shared harness — `api/internal/testsupport/rlsdb` (D7)

Generalizes `cv/ingest_integration_test.go`'s plumbing into reusable helpers so each domain test is ~30-50 lines of assertions, not 90 of boilerplate.

```go
package rlsdb

// Harness holds the two pools and skips the test if TEST_DATABASE_URL is unset.
type Harness struct {
    AppPool   *pgxpool.Pool // app_user — RLS ENFORCED. Exercises the Service.
    AdminPool *pgxpool.Pool // superuser (creds swapped in DSN) — seeds + asserts ground truth.
}

// New returns a Harness or calls t.Skip when TEST_DATABASE_URL is absent.
func New(ctx context.Context, t *testing.T) *Harness

// SeedUser creates a real user via auth_upsert_user (SECURITY DEFINER, bypasses
// RLS exactly like production OAuth signup).
func (h *Harness) SeedUser(ctx, t, email, googleID string) uuid.UUID

// EnsurePgbossStandin creates the minimal pgboss.job table + grants (for enqueue paths).
func (h *Harness) EnsurePgbossStandin(ctx, t)

// Exec / QueryRow on AdminPool for ground-truth seeding + assertions.
```

DSN derivation, `auth_upsert_user` seeding, and the `pgboss.job` stand-in are lifted verbatim from the cv test (lines 41-66, 158-189). The `cv` test can be refactored onto this later — **NOT in scope here; leave cv green**.

### 4.2 Per-domain integration tests (one file each)

`{domain}/rls_integration_test.go`, each `package {domain}_test`, each `rlsdb.New(...)`-gated. Per-domain assertions:

| Domain | Owner-success assertion | Cross-tenant / empty-GUC assertion |
|---|---|---|
| `auth` | `GetUserByID(userA)` returns userA's row | `GetUserByID(userB's id)` from a tenant tx set to userA → denied/not-found (proves the refresh read is RLS-gated, not raw) |
| `jobs` | A adds + lists + gets own job | B's `GetByID(A's jobID)` → `ErrNotFound`; B's `List` excludes A's jobs |
| `scan` | A triggers + `GetScanRun(own)` succeeds | **B's `GetScanRun(A's runID)` → `ErrNotFound` (the IDOR proof, success criterion #2)** |
| `evaluate` | A enqueues for own job; A reads own report | B's `GetReport(A's jobID)` → `ErrNotFound`; B cannot enqueue against A's job |
| `companies` | A adds + lists + removes own | **B's `Remove(A's companyID)` does NOT delete A's row** (DeleteWatchedCompany has no `WHERE user_id` — RLS-only); assert A's row still present via AdminPool |
| `tracker` | A updates own application | **B's `UpdateApplication(A's appID, …)` → `ErrNotFound` AND A's row unchanged** (WITH CHECK proof, success criterion #3) |
| `cv` (remaining) | A creates/lists/sets-master own CV | B's `SetMasterCV`/`GetDownloadURL` against A's objects → `ErrNotFound` |

Every test asserts the **owner path still works** (behavior-preserving, success criterion #6) and the **non-owner path is denied at the DB**, exercised as `app_user` (never superuser, or the assertion is a false positive).

### 4.3 pgTAP stays as the policy-layer proof

`db/tests/*_rls.test.sql` remain the DB-isolated proof that policies + FORCE flags enforce isolation. Add one assertion to the existing/relevant pgTAP test (or a small new one) proving the **NULLIF behavior**: with `app.current_user_id` set to `''`, a SELECT returns 0 rows and does NOT raise `22P02`. `make test-rls` must stay green.

### 4.4 Existing mock-based tests

All `handler_test.go` / mock `Servicer` tests are unchanged — signatures are preserved. They keep proving handler↔service contract; they cannot prove RLS (that is what 4.2 is for). `go-testing`: mock at the Servicer boundary for handlers, real DB for the RLS invariant.

---

## 5. Slicing for review budget (seam map — `sdd-tasks` owns the task list)

Honors the 400-line guard. Dependency edges drawn so every service slice merges onto a DB where NULLIF + the shared helper already exist.

```
PR-0  spike (D9)            ── verify 22P02 vs deny live ── (throwaway, ~0 prod lines)
   │
PR-1  foundation           ── 003 migration + rls.sql mirror + platform.WithTenantTx
   │     (+ delete old WithTenant) + rlsdb harness + pgTAP NULLIF assertion
   │     dedupe cv.withTenant → WithTenantTx (D3)
   │     ~150-200 lines. MUST land first.
   │
   ├─ PR-2  scan slice      ── GetScanRun + TriggerScan wired + scan rls test
   │          (highest-priority security; IDOR already hotfixed, now DB-gated)
   ├─ PR-3  tracker slice   ── UpdateApplication ordering fix (D8) + ListApplications + test
   ├─ PR-4  auth slice      ── GetUserByID wired (D6) + test
   ├─ PR-5  jobs slice      ── AddManual/List/GetByID + repo-from-tx + test
   ├─ PR-6  evaluate slice  ── EnqueueEvaluation/GetReport + test
   ├─ PR-7  companies slice ── List/Add/Remove + test
   └─ PR-8  cv slice        ── remaining 5 methods (GetDownloadURL R2-outside-tx) + test
```

- **PR-1 is the hard prerequisite** for PR-2…PR-8 (they all call `platform.WithTenantTx` and rely on NULLIF for safe mixed-state).
- PR-2…PR-8 are **independent of each other** — parallelizable / stackable in any order once PR-1 is in. Order by security priority: scan → tracker → auth first.
- Each service slice is ~50-120 lines (service edits + one integration test). Comfortably under 400.
- Estimated total hand-written: ~700-850 lines (helper+migration+harness ~200, 7 service slices ~80 avg = ~560, +pgTAP). **Over 400 → chained PRs along the seams above.** `sdd-tasks` produces the Review Workload Forecast.

---

## 6. Loose ends to clean up (fold into PR-1)

- **`middleware/tenant.go`** — `TenantIsolation` is a no-op with a stale comment referencing `platform.WithTenant`. Options: (a) delete the middleware + its `main.go:83` mount, or (b) leave it but fix the misleading comment. Recommend **(a) delete** — it gives a false sense that tenancy is handled at the middleware layer when the real mechanism is now `WithTenantTx` at the service layer. Verify nothing else depends on the mount (it only checks `userID` presence, already done by `Authenticator`). If deletion risks scope creep, downgrade to (b) and fix the comment. `sdd-tasks` decides.

---

## 7. Risks & edge cases

| Risk | Severity | Mitigation |
|---|---|---|
| **Empty-string GUC contamination across pooled connections** — a tenant tx leaves `app.current_user_id=''`; a later query on the same physical connection hits `''::uuid` → `22P02` → 500. | High | **D5: NULLIF migration lands FIRST (PR-1).** Makes `'' → deny`. This is the prerequisite the whole change rests on. |
| **Live failure mode unverified** (22P02 vs silent empty vs partial) — never reproduced (no DB in explore). | High | **D9: spike as the FIRST task (PR-0).** Stand up careerops container, confirm `22P02` on unset/empty GUC, confirm NULLIF converts to clean deny. De-risks PR-1 ordering. |
| **`cv.GetDownloadURL` holds a pooled connection across R2 network I/O** if the R2 signing call were inside the tenant tx. | Medium | **§2: read the application row inside `WithTenantTx`, exit the tx, THEN call `r2.SignedDownloadURL`.** Never do external I/O inside the tx. This is the concrete reason proposal Approach 2 (request-wide tx) was rejected. |
| **Per-request tx + connection overhead** — every single-statement read now runs in a BEGIN/COMMIT. | Low | Accepted trade-off (proposal). MVP control-plane traffic is low; correctness > a sub-ms tx overhead. `stdlib.OpenDBFromPool` reuses the pgxpool; no new pool. |
| **`platform` → `internal/db` import edge** could create a cycle. | Low | One-way: `db` (sqlc-generated) imports nothing from `platform`. Verified. `cv` already had this edge. |
| **Partially-wired window between slices** — some domains wrapped, others still raw on the same pool. | Low | NULLIF (PR-1) makes the mixed state SAFE (empty GUC → deny, not 500). Slices always merge onto a NULLIF DB. |
| **Dropping `tracker`'s post-UPDATE check (D8)** could remove a guard if RLS were ever disabled. | Low | RLS is FORCE'd on every tenant table and pgTAP-proven. The check ran AFTER the unscoped UPDATE — it was never the real guard. Keep one line as backstop if a reviewer insists; recommend drop for clarity. |
| **`auth.GetUserByID` raw-SQL vs sqlc choice** affects diff size + whether `sqlc generate` runs. | Low | §2: (b) wrap the existing raw `SELECT` in the tx for a minimal diff, or (a) add a sqlc query for consistency. `sdd-tasks` picks per budget. |

---

## 8. Success criteria mapping (from proposal)

| # | Criterion | Where satisfied |
|---|---|---|
| 1 | One DB-gated integration test per domain, denial proven as `app_user` | §4.2 (7 files) + §4.1 harness |
| 2 | `scan.GetScanRun` returns 404 cross-tenant | §2 scan + §4.2 scan test (PR-2) |
| 3 | No mutation relies on app-layer check as sole guard | §2 tracker/companies (D8) + §4.2 WITH-CHECK tests |
| 4 | `make test-rls` green after NULLIF | §3.2 + §4.3 pgTAP NULLIF assertion |
| 5 | No service method touches a tenant table over the raw pool | §2 (all wrapped); raw pool reserved for `queue.Enqueue` |
| 6 | Same-tenant behavior unchanged | §2 (signatures preserved) + §4.4 existing tests green + every §4.2 owner-path assertion |

## Next step

Run `sdd-tasks` (reads `spec.md` + this `design.md`). Tasks should emit the PR-0 spike FIRST, then PR-1 foundation, then the per-domain slices in §5 order, and produce a Review Workload Forecast flagging the ~700-850-line budget → chained PRs along the PR-1…PR-8 seams.
