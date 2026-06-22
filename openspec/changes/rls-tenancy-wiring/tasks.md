# Tasks — `rls-tenancy-wiring`

> Phase: tasks · Status: complete · Artifact store: openspec
> Input: `openspec/changes/rls-tenancy-wiring/spec.md`, `openspec/changes/rls-tenancy-wiring/design.md`, `openspec/changes/rls-tenancy-wiring/proposal.md`
> Task ID range: **T-117 .. T-156** (contiguous, fresh range — highest existing ID found in repo is `T-116` in `openspec/changes/ingest-cv/tasks.md`)
> Strict TDD is active (`make test-all`). Every implementation task is preceded by its test task. Order within each seam is test-first.
> The `scan.GetScanRun` app-layer IDOR hotfix is already merged (PR #8) — Seam scan engages DB-level RLS **on top of** that check (defense-in-depth), it does not re-add the app-layer guard.

## Seam map (PR boundaries)

| Seam | Scope | Spec requirements covered |
|------|-------|----------------------------|
| **0** | Live-DB spike — verify 22P02-vs-deny empirically | De-risks R4 ordering (D9); no prod lines |
| **1** | Foundation — NULLIF migration 003 + `platform.WithTenantTx` + shared `rlsdb` test harness + migrate `cv.withTenant` onto it + delete dead code | R1 (helper), R4 (NULLIF), R6 (dead-code cleanup) |
| **2** | `scan` slice — `GetScanRun` + `TriggerScan` | R2 (scan.GetScanRun), R5 (scan.TriggerScan unchanged) |
| **3** | `tracker` slice — `UpdateApplication` ordering fix + `ListApplications` | R3 (tracker WITH CHECK), R5 (owner update unchanged) |
| **4** | `auth` slice — `GetUserByID` | R2 (auth.GetUserByID), R6 (UpsertUser untouched, proven by omission) |
| **5** | `jobs` slice — `AddManual` / `List` / `GetByID` + repo-from-tx | R2 (jobs.GetByID), R5 (jobs.List unchanged) |
| **6** | `evaluate` slice — `EnqueueEvaluation` / `GetReport` | R2 (evaluate.GetReport), R5 (evaluate.EnqueueEvaluation unchanged) |
| **7** | `companies` slice — `List` / `Add` / `Remove` | R2 (companies.List), R3 (companies.Remove WITH CHECK) |
| **8** | `cv` slice — remaining 5 methods (`GetDownloadURL`, `ListCVs`, `CreateCV`, `SetMasterCV`, `EnqueuePDFGeneration`) | R2 (cv non-ingest reads), R5 (cv owner paths unchanged) |
| **9** | Cross-seam final verification | All requirements, end to end |

Dependency edges: **Seam 0 → Seam 1** (the spike's empirical result confirms the NULLIF migration is necessary and sufficient before anyone codes against it). **Seam 1 → Seams 2–8** (every domain slice calls `platform.WithTenantTx`, which does not exist until Seam 1 lands, and relies on NULLIF for safe mixed-state during rollout). **Seams 2–8 are mutually independent** — parallelizable/stackable in any order once Seam 1 is merged; this checklist orders them 2→8 by security priority (scan and tracker first, since they are the two domains with an identified IDOR/WITH-CHECK gap). **Seam 9 depends on all of 0–8.**

---

## Seam 0 — Live-DB spike (D9)

**Spec coverage:** De-risks Requirement 4's ordering assumption. No spec scenario is satisfied directly by this seam — it is a throwaway verification step that confirms the failure mode the rest of the change is designed around.

**Depends on:** nothing (can run immediately, against the existing `docker compose up` Postgres).

| ID | Type | Description | File(s) |
|----|------|--------------|---------|
| T-117 | spike | Stand up the `careerops` Postgres container; as `app_user`, run a query against an RLS-protected table (1) with `app.current_user_id` never set, (2) with it set then the transaction ended (GUC reverts to `''`) on the same pooled connection. Record whether each case raises `22P02 invalid input syntax for type uuid` or returns 0 rows cleanly. Then apply the Seam-1 NULLIF migration manually (or a throwaway copy of it) and re-run case (2), confirming it now returns 0 rows without erroring. | n/a (manual spike; no committed file — findings inform Seam 1's commit message / fold into pgTAP test rationale in T-122) |

**Sequencing within 0:** single task, blocking for Seam 1.

---

## Seam 1 — Foundation: NULLIF migration + `platform.WithTenantTx` + shared test harness

**Spec coverage:** Requirement 1 (shared tenant-tx helper, all scenarios), Requirement 4 (NULLIF hardening, all scenarios), Requirement 6 (dead-code removal so no stale tenancy mechanism remains — `platform.WithTenant`, `middleware/tenant.go`).

**Depends on:** Seam 0 (spike confirms the failure mode this migration fixes).

| ID | Type | Description | File(s) |
|----|------|--------------|---------|
| T-118 | test | [x] Write a pgTAP assertion that with `app.current_user_id` set to `''` (empty string, simulating a reset GUC on a pooled connection), a SELECT against an RLS-protected table (e.g. `jobs`) returns 0 rows and does **not** raise `22P02` — Req 4 scenario "Empty-string GUC denies cleanly instead of erroring" | `db/tests/nullif_guc.test.sql` (new) |
| T-119 | impl | [x] Add `db/migrations/003_rls_nullif.sql` — DROP + CREATE all 9 tenant policies (`tenant_users`, plus the 8 keyed on `user_id`: `watched_companies`, `jobs`, `applications`, `reports`, `cvs`, `scan_runs`, `usage`, `cv_ingestions`), each `USING`/`WITH CHECK` migrated to `NULLIF(current_setting('app.current_user_id', true), '')::uuid`. ENABLE/FORCE flags untouched — makes T-118 green | `db/migrations/003_rls_nullif.sql` (new) |
| T-120 | impl | [x] Mirror the same 9 policy bodies into `db/rls.sql` (bootstrap source of truth) so a fresh `docker compose up` DB matches a migrated one. No change to `db/schema.sql` (verified: contains no `CREATE POLICY`/`FORCE ROW LEVEL` lines) | `db/rls.sql` |
| T-121 | test | [x] Write/extend a pgTAP regression assertion confirming a **properly-set** GUC still returns only the matching tenant's rows after the NULLIF migration (no happy-path regression) — Req 4 scenario "NULLIF migration does not change behavior for a properly-set GUC" | `db/tests/nullif_guc.test.sql` (same file, second assertion) |
| T-122 | verify | [x] Run `make test-rls` — confirm T-118/T-121 pass AND every pre-existing pgTAP assertion (per-table cross-tenant invisibility, FORCE RLS flags) is still green with no test changes required — Req 4 scenario "pgTAP RLS tests stay green after the NULLIF migration" — run by orchestrator: 25+6+4=35 assertions, all PASS | n/a (verification step) |
| T-123 | test | [x] Write a Go test for `platform.WithTenantTx(ctx, pool, userID, fn)` against a live test DB (gated, skip without `TEST_DATABASE_URL`): asserts `set_config('app.current_user_id', ..., true)` is applied before `fn` runs, commits on `fn` returning nil, rolls back on `fn` returning an error — Req 1 scenario "GUC-scoped query returns only the matching tenant's rows" (helper-level proof, generalized from the cv precedent) | `api/internal/platform/postgres_test.go` (new) |
| T-124 | impl | [x] Add `platform.WithTenantTx(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, fn func(q *db.Queries) error) error` to `api/internal/platform/postgres.go` — body is the proven `cv.withTenant` lifted one level, using `stdlib.OpenDBFromPool` + `db.New(tx)` — makes T-123 green | `api/internal/platform/postgres.go` |
| T-125 | impl | [x] Delete the dead `platform.WithTenant(ctx, pool, userID string, fn func(*pgx.Conn) error)` (zero call sites, `fmt.Sprintf`-interpolated `SET LOCAL`, returns raw `*pgx.Conn`). Drop the now-unused `github.com/jackc/pgx/v5` import from `postgres.go` if nothing else in the file needs it | `api/internal/platform/postgres.go` |
| T-126 | impl | [x] Delete `api/internal/middleware/tenant.go` (`TenantIsolation` no-op middleware with a stale comment referencing the just-deleted `platform.WithTenant`) and its mount point. Verify via grep that nothing else depends on the mount (it only checked `userID` presence, already covered by `Authenticator`) | `api/internal/middleware/tenant.go` (deleted), `api/cmd/.../main.go` (remove mount — confirm exact path during apply) |
| T-127 | impl | [x] Re-point `cv.withTenant`'s 2 call sites at `platform.WithTenantTx(ctx, s.pool, userID, fn)` directly; delete the now-redundant `cv.withTenant` method and its doc comment referencing "Seam-B-local fix" (superseded — the gap it called out is now closed package-wide) | `api/internal/cv/service.go` |
| T-128 | verify | [x] Run `cd api && go test ./internal/cv/... -count=1` — confirm the existing `cv` ingest tests (T-85..T-94 from `ingest-cv`) still pass unmodified after the `withTenant` → `WithTenantTx` re-point (D3 — zero behavioral change) | n/a (verification step) |
| T-129 | impl | [x] Create the shared DB-gated test harness package `api/internal/testsupport/rlsdb`: `Harness{AppPool, AdminPool}`, `New(ctx, t) *Harness` (skips via `t.Skip` without `TEST_DATABASE_URL`), `SeedUser(ctx, t, email, googleID) uuid.UUID` (via `auth_upsert_user`, SECURITY DEFINER), `EnsurePgbossStandin(ctx, t)` — generalized from `cv/ingest_integration_test.go` lines 41-66/158-189. No test asserts on the harness itself; it is exercised transitively by every Seam 2-8 integration test | `api/internal/testsupport/rlsdb/harness.go` (new) |
| T-130 | verify | [x] Run `make test-all` once for the whole foundation seam — confirms migration 003 + `rls.sql` mirror + `WithTenantTx` + dead-code removal + `cv` re-point + new harness package all compile/pass together before any domain slice starts consuming them — `make test-go` run directly (Go unit suite only, no Docker, per apply constraints); worker/web untouched by this seam; `make test-rls` deferred to orchestrator | n/a (verification step) |

**Sequencing within 1:** T-118 → T-119 → T-120 → T-121 → T-122 (DB/migration sub-chain, strictly sequential) → T-123 → T-124 → T-125 → T-126 → T-127 → T-128 (Go helper + dead-code sub-chain, strictly sequential — each depends on the prior file state) → T-129 → T-130. The DB sub-chain and the Go sub-chain touch disjoint files and could be developed in parallel, but T-124 (`WithTenantTx`) has no functional dependency on T-119/T-120 landing first — only the *commit/merge* of migration 003 must precede Seam 2-8 service code being merged (so the deployed DB is never tenant-tx-wrapped without NULLIF). Recommend landing the DB sub-chain and Go sub-chain in the same PR-1 to keep that invariant simple to reason about.

---

## Seam 2 — `scan` slice

**Spec coverage:** Requirement 2 (`scan.GetScanRun` DB-layer denial, in addition to the existing app-layer check), Requirement 5 (`scan.TriggerScan` unchanged).

**Depends on:** Seam 1 (`platform.WithTenantTx`, `rlsdb` harness).

| ID | Type | Description | File(s) |
|----|------|--------------|---------|
| T-131 | test | [x] Write `scan/rls_integration_test.go` using the `rlsdb` harness: seed users A and B, seed a `scan_runs` row for A via `AdminPool`; assert `scan.Service.GetScanRun` called as B against A's run ID returns `ErrNotFound`/zero rows from Postgres itself (RLS denial), independent of the existing app-layer `scanRun.UserID != userID` check; assert A's own `GetScanRun` still succeeds; assert `TriggerScan` for A still inserts a `scan_runs` row + the response shape is unchanged — Req 2 scenario "scan.GetScanRun is denied at the DB layer in addition to the existing app-layer check", Req 5 scenario "scan.TriggerScan still enqueues and inserts correctly" | `api/internal/scan/rls_integration_test.go` (new) |
| T-132 | impl | [x] Wire `scan.Service.GetScanRun`'s `GetScanRunByID` call through `platform.WithTenantTx`; wire `scan.Service.TriggerScan`'s `ListEnabledWatchedCompaniesByUser` + `InsertScanRun` calls through the same tenant tx, keeping the `queue.Enqueue` loop (one job per company) **outside** the tx, after commit. Keep the existing app-layer `scanRun.UserID != userID` check in `GetScanRun` as defense-in-depth (do not remove — it is the merged PR #8 IDOR fix) — makes T-131 green | `api/internal/scan/service.go` |
| T-133 | verify | [x] Run `cd api && go test ./internal/scan/... -count=1` — confirm existing mock-based `handler_test.go` assertions pass unmodified (Req 5 scenario "Existing mock-based handler/service tests still pass") alongside the new integration test — run against a live, NULLIF-migrated DB (`docker compose up -d postgres`); all 9 tests (8 mock-based + 1 new integration test, 3 subtests) PASS; skips cleanly without `TEST_DATABASE_URL` | n/a (verification step) |

**Sequencing within 2:** T-131 → T-132 → T-133, strictly sequential.

---

## Seam 3 — `tracker` slice

**Spec coverage:** Requirement 3 (`tracker.UpdateApplication` WITH CHECK, both denial and owner-success scenarios), Requirement 5 (owner update unchanged).

**Depends on:** Seam 1.

| ID | Type | Description | File(s) |
|----|------|--------------|---------|
| T-134 | test | [x] Write `tracker/rls_integration_test.go`: seed an `applications` row owned by A; assert B's `UpdateApplication` call against that row's ID affects zero rows (RLS `USING` excludes it) and the service returns `ErrNotFound`; assert A's row is verified unchanged via `AdminPool` after B's attempt; assert A's own `UpdateApplication` against the same row still succeeds and is visible on a subsequent read — Req 3 scenarios "tracker.UpdateApplication cannot mutate another tenant's row" + "Owner mutation still succeeds" | `api/internal/tracker/rls_integration_test.go` (new) |
| T-135 | impl | [x] Move `tracker.Service.UpdateApplication`'s UPDATE statement(s) (`UpdateApplicationStatus`/`UpdateApplicationNotes`) inside `platform.WithTenantTx`, both in the same tx if both fire. Drop the post-UPDATE `if updated.UserID != userID` check (D8 — it ran after an unscoped UPDATE and is now dead-for-security; RLS `WITH CHECK`/`USING` is the real guard) — makes T-134 green | `api/internal/tracker/service.go` |
| T-136 | verify | [x] Run `cd api && go test ./internal/tracker/... -count=1` — confirm `ListApplications` (also wired through `WithTenantTx` for `ListApplicationsByUser`) and existing mock-based handler tests pass unmodified — all 10 tests pass (9 pre-existing + 1 new integration test, 2 subtests); skips cleanly without `TEST_DATABASE_URL`; re-ran 3x for stability; `make test-go` full unit suite clean | n/a (verification step) |

**Sequencing within 3:** T-134 → T-135 → T-136, strictly sequential.

---

## Seam 4 — `auth` slice

**Spec coverage:** Requirement 2 (`auth.GetUserByID` cross-tenant denial, refresh-token path), Requirement 6 (proof by omission that `auth.UpsertUser` is untouched).

**Depends on:** Seam 1.

| ID | Type | Description | File(s) |
|----|------|--------------|---------|
| T-137 | test | Write `auth/rls_integration_test.go`: seed users A and B via `rlsdb.SeedUser` (which itself calls `auth_upsert_user`, proving that path stays untouched/SECURITY DEFINER); assert `auth.Service.GetUserByID` run under a tenant tx scoped to B but asked to look up A's user ID returns no row / a not-found condition; assert `GetUserByID` scoped to A returns A's own row correctly — Req 2 scenario "auth.GetUserByID denies cross-tenant read (refresh-token path)" | `api/internal/auth/rls_integration_test.go` (new) |
| T-138 | impl | Wrap `auth.GetUserByID`'s existing raw `SELECT` in `platform.WithTenantTx` (minimal-diff option (b) from design §2 — no sqlc regen required for this slice). Map `sql.ErrNoRows` to the existing not-found error path. Do not touch `auth.UpsertUser`/`auth_upsert_user` — makes T-137 green | `api/internal/auth/service.go` |
| T-139 | verify | Run `cd api && go test ./internal/auth/... -count=1` — confirm existing auth tests (OAuth flow, refresh rotation) pass unmodified | n/a (verification step) |

**Sequencing within 4:** T-137 → T-138 → T-139, strictly sequential.

---

## Seam 5 — `jobs` slice

**Spec coverage:** Requirement 2 (`jobs.GetByID` cross-tenant denial), Requirement 5 (`jobs.List` unchanged).

**Depends on:** Seam 1.

| ID | Type | Description | File(s) |
|----|------|--------------|---------|
| T-140 | test | Write `jobs/rls_integration_test.go`: seed a `jobs` row owned by A; assert B's `jobs.Service.GetByID` for that job's ID returns zero rows from `GetJobByID` and the handler-level response is `404 Not Found`; assert A's own `List`/`GetByID`/`AddManual` still succeed and `List` returns only A's jobs — Req 2 scenario "jobs.GetByID denies cross-tenant read", Req 5 scenario "jobs.List returns the caller's own jobs" | `api/internal/jobs/rls_integration_test.go` (new) |
| T-141 | impl | Add `newRepoFromQueries(q *db.Queries) *Repo` constructor; wire `jobs.Service.AddManual`/`List`/`GetByID` to build the `Repo` from inside `platform.WithTenantTx`'s `fn` closure instead of `s.repo()` over the raw pool. Remove `jobs/repo.go`'s now-unused `NewRepo(pool)`/`newRepoFromSQL` if no other caller remains (verify via grep) — makes T-140 green | `api/internal/jobs/service.go`, `api/internal/jobs/repo.go` |
| T-142 | verify | Run `cd api && go test ./internal/jobs/... -count=1` — confirm existing platform-detection and handler tests pass unmodified | n/a (verification step) |

**Sequencing within 5:** T-140 → T-141 → T-142, strictly sequential.

---

## Seam 6 — `evaluate` slice

**Spec coverage:** Requirement 2 (`evaluate.GetReport` cross-tenant denial), Requirement 5 (`evaluate.EnqueueEvaluation` unchanged).

**Depends on:** Seam 1.

| ID | Type | Description | File(s) |
|----|------|--------------|---------|
| T-143 | test | Write `evaluate/rls_integration_test.go`: seed a `jobs`→`applications`→`reports` chain owned by A; assert B's `GetReport` for that job ID returns zero rows / not-found across the chain; assert A's own `GetReport` succeeds; assert A's own `EnqueueEvaluation` (job lookup + usage read, then enqueue) is unchanged — Req 2 scenario "evaluate.GetReport denies cross-tenant read", Req 5 scenario "evaluate.EnqueueEvaluation still succeeds for the owner" | `api/internal/evaluate/rls_integration_test.go` (new) |
| T-144 | impl | Wire `evaluate.Service.GetReport`'s 3-step chain (`GetJobByID` → `GetApplicationByJobID` → `GetReportByApplicationID`) inside ONE `platform.WithTenantTx` call so RLS is consistent across all three reads; wire `EnqueueEvaluation`'s `GetJobByID` + `GetUsageByUserMonth` inside one tenant tx, with `queue.Enqueue` called after commit (mirrors `cv.EnqueueIngest`) — makes T-143 green | `api/internal/evaluate/service.go` |
| T-145 | verify | Run `cd api && go test ./internal/evaluate/... -count=1` — confirm existing usage-limit and handler tests pass unmodified | n/a (verification step) |

**Sequencing within 6:** T-143 → T-144 → T-145, strictly sequential.

---

## Seam 7 — `companies` slice

**Spec coverage:** Requirement 2 (`companies.List` no cross-tenant leakage), Requirement 3 (`companies.Remove` WITH CHECK / RLS-only mutation guard).

**Depends on:** Seam 1.

| ID | Type | Description | File(s) |
|----|------|--------------|---------|
| T-146 | test | Write `companies/rls_integration_test.go`: seed `watched_companies` rows for A and B; assert B's `List` returns only B's companies, never A's; assert B's `Remove` against A's company ID deletes zero rows (since `DeleteWatchedCompany` has no `WHERE user_id` clause — RLS is the only guard) and A's row is verified still present via `AdminPool`; assert A's own `Add`/`List`/`Remove` still succeed — Req 2 scenario "companies.List/Add scoped correctly, no cross-tenant leakage", Req 3 scenario "companies.Remove cannot delete another tenant's row" | `api/internal/companies/rls_integration_test.go` (new) |
| T-147 | impl | Wire `companies.Service.List`/`Add` through `platform.WithTenantTx`; wire `Remove`'s `GetWatchedCompanyByID` + `DeleteWatchedCompany` inside ONE tenant tx (both statements must share the same GUC-scoped tx since the DELETE has no app-layer WHERE) — makes T-146 green | `api/internal/companies/service.go` |
| T-148 | verify | Run `cd api && go test ./internal/companies/... -count=1` — confirm existing provider-detection and handler tests pass unmodified | n/a (verification step) |

**Sequencing within 7:** T-146 → T-147 → T-148, strictly sequential.

---

## Seam 8 — `cv` slice (remaining non-ingest methods)

**Spec coverage:** Requirement 2 (`cv.GetDownloadURL`/`ListCVs` cross-tenant denial), Requirement 5 (`cv.CreateCV`/`ListCVs`/`EnqueuePDFGeneration` owner paths unchanged).

**Depends on:** Seam 1 (the `cv.EnqueueIngest`/`GetIngestion` re-point already happened in T-127 — this seam covers the 5 *other* methods that still used `s.queries()` over the raw pool).

| ID | Type | Description | File(s) |
|----|------|--------------|---------|
| T-149 | test | Write `cv/rls_integration_test.go`: seed a `cvs` row owned by A; assert B's `GetDownloadURL`/`ListCVs`/`SetMasterCV` against A's CV ID return zero rows / not-found; assert A's own `CreateCV`, `ListCVs`, `SetMasterCV`, `EnqueuePDFGeneration` still succeed unchanged — Req 2 scenario "cv non-ingest methods deny cross-tenant read", Req 5 scenarios "cv.CreateCV / ListCVs still succeed for the owner" + "cv.EnqueuePDFGeneration still succeeds for the owner" | `api/internal/cv/rls_integration_test.go` (new) |
| T-150 | impl | Wire `cv.Service.ListCVs`, `CreateCV`, `SetMasterCV` through `platform.WithTenantTx`; wire `EnqueuePDFGeneration`'s 3 reads (`GetApplicationByJobID` + `GetReportByApplicationID` + `GetMasterCVByUser`) inside ONE tenant tx, with `queue.Enqueue` after commit — makes the corresponding parts of T-149 green | `api/internal/cv/service.go` |
| T-151 | impl | Wire `cv.Service.GetDownloadURL`'s `GetApplicationByJobID` read inside `platform.WithTenantTx`, capturing `pdf_path` before the tx exits; call `r2.SignedDownloadURL(...)` strictly **after** the tx commits/exits — never hold a pooled connection across the R2 network round-trip — makes the remaining part of T-149 green | `api/internal/cv/service.go` |
| T-152 | verify | Run `cd api && go test ./internal/cv/... -count=1` — confirm both the existing ingest tests (Seam 1's T-128 re-check) and the new non-ingest RLS tests pass together | n/a (verification step) |

**Sequencing within 8:** T-149 → T-150 → T-151 → T-152, strictly sequential. T-150 and T-151 both make parts of the same test file green but are split because `GetDownloadURL`'s R2-outside-tx constraint is a distinct, higher-risk edit worth its own commit.

---

## Cross-seam final verification (after all seams land)

| ID | Type | Description |
|----|------|--------------|
| T-153 | verify | Run `make test-all` (Go + worker + web unit suites) — confirm zero regression across every domain touched by Seams 1-8 |
| T-154 | verify | Run `make test-rls` (pgTAP) — confirm the NULLIF migration (T-119/T-120) and all pre-existing per-table RLS assertions are green together as the final committed state |
| T-155 | verify | Run every per-domain DB-gated integration test (`go test ./... -count=1` across `auth`, `jobs`, `scan`, `evaluate`, `companies`, `tracker`, `cv` with `TEST_DATABASE_URL` set against a live Postgres) — confirm all Seam 2-8 integration tests (T-131, T-134, T-137, T-140, T-143, T-146, T-149) pass together against the same fully-migrated DB, not just individually against a freshly-reset one |
| T-156 | verify | Grep-audit: confirm no remaining call site in `api/internal/{auth,jobs,scan,evaluate,companies,tracker,cv}` builds `*db.Queries` over the raw pool for a tenant-table query (i.e., `s.queries()`/`s.repo()`-over-pool patterns are gone), and that `queue.Enqueue` is the only remaining raw-pool caller — Req 1 scenario "No tenant-table service method bypasses the tenant-tx helper" |

---

## Review Workload Forecast

| Seam | Hand-written lines (impl + test) | Notes |
|------|-----------------------------------|-------|
| 0 — spike | 0 | Manual verification, no committed prod/test lines |
| 1 — Foundation (migration + helper + harness + dead-code removal + cv re-point) | ~210 (migration ~45, rls.sql mirror ~20, `WithTenantTx` ~30, dead-code deletion -40/+0, middleware deletion -35/+0, cv re-point ~10, rlsdb harness ~90) | pgTAP test ~25 tracked in test budget below; this is the hard prerequisite, largest single seam |
| 2 — scan | ~70 (service ~30, integration test ~40) | Highest security priority; IDOR already hotfixed, this adds the DB-layer gate |
| 3 — tracker | ~75 (service ~25, integration test ~50) | WITH CHECK proof; drops dead post-UPDATE check (net small) |
| 4 — auth | ~55 (service ~15, integration test ~40) | Smallest service diff — minimal-diff raw-SQL-in-tx option chosen |
| 5 — jobs | ~90 (service+repo ~40, integration test ~50) | Repo-from-tx indirection adds a bit of surface |
| 6 — evaluate | ~85 (service ~35, integration test ~50) | Two methods, one a 3-step chain |
| 7 — companies | ~75 (service ~25, integration test ~50) | RLS-only DELETE guard is the key proof |
| 8 — cv (remaining 5 methods) | ~100 (service ~40, integration test ~60) | Split into 2 impl tasks for the R2-outside-tx risk |
| Cross-seam verification (9) | 0 | Verification-only, no new lines |
| **Total (hand-written)** | **~760** | |

**Chained PRs recommended: Yes**
**400-line budget risk: High**
**Decision needed before apply: Yes**

### Recommended PR sequence (dependency edges)

```
PR-0  spike (D9)                         ── ~0 lines, throwaway
   │
PR-1  foundation (T-118..T-130)          ── ~210+25 test = ~235 lines   [MUST land first]
   │
   ├──► PR-2  scan      (T-131..T-133)   ── ~70 lines   [depends on PR-1 only]
   ├──► PR-3  tracker    (T-134..T-136)  ── ~75 lines   [depends on PR-1 only]
   ├──► PR-4  auth       (T-137..T-139)  ── ~55 lines   [depends on PR-1 only]
   ├──► PR-5  jobs       (T-140..T-142)  ── ~90 lines   [depends on PR-1 only]
   ├──► PR-6  evaluate   (T-143..T-145)  ── ~85 lines   [depends on PR-1 only]
   ├──► PR-7  companies  (T-146..T-148)  ── ~75 lines   [depends on PR-1 only]
   └──► PR-8  cv         (T-149..T-152)  ── ~100 lines  [depends on PR-1 only]
            │
            └──► PR-9 final verification (T-153..T-156) ── 0 lines, runs after all of 2-8 are merged
```

Each individual PR lands comfortably under the 400-line budget (largest is PR-1 at ~235 lines). PR-2 through PR-8 are **independent of each other** — no domain slice's diff or test depends on another domain's slice — so they can be reviewed and merged in any order, or in parallel by different reviewers, once PR-1 is in.

**Sequencing rationale:**
1. **PR-0 first** — confirms the empirical 22P02-vs-deny failure mode before committing to the NULLIF migration's necessity/shape.
2. **PR-1 is the hard prerequisite** — `platform.WithTenantTx` does not exist, and NULLIF is not applied, until this lands. No domain slice can be written against it before this merges.
3. **PR-2 (scan) and PR-3 (tracker) are the highest security priority** — scan has a known (already-hotfixed-at-app-layer) IDOR gaining a DB-layer backstop; tracker has a known WITH-CHECK gap on UPDATE. Recommend landing these first among the domain slices, but they do not block one another.
4. **PR-4 through PR-8 can land in any order** relative to each other and relative to PR-2/PR-3 — all seven domain slices share only the PR-1 dependency.
5. **PR-9 (final verification) runs last**, after all of PR-2..PR-8 are merged, to prove the fully-wired system end to end.

### Recommended chain strategy

**Stacked-to-main** fits this change well: PR-1 alone is a complete, mergeable, behavior-preserving foundation (the NULLIF migration tightens an existing gap with zero functional change to the happy path; the new helper and harness are unused dead-code-replacement until a domain slice consumes them — but they compile and their own tests pass standalone). Each of PR-2 through PR-8 is independently mergeable and individually closes one domain's RLS gap — none requires a feature flag or "do not merge until everything is ready" tracker, and a partially-rolled-out state (some domains wired, others still on the raw pool) is explicitly safe because PR-1's NULLIF migration makes the raw-pool path degrade to clean denial rather than error. **Feature-branch-chain** would only be warranted if the team wanted to gate visible behavior change (e.g., a security audit sign-off) behind one combined merge to `main` — not the case here, since this change is enforcement-only and each slice closes a real gap the moment it lands. Final choice is left to the orchestrator/user per `ask-on-risk`.

---

## Traceability summary

| Spec Requirement | Task IDs |
|-------------------|----------|
| R1 — Shared tenant-tx helper, no raw-pool tenant queries | T-123, T-124, T-125, T-127, T-129, T-156 |
| R2 — Cross-tenant read denial per domain | T-131, T-132 (scan), T-137, T-138 (auth), T-140, T-141 (jobs), T-143, T-144 (evaluate), T-146, T-147 (companies), T-149, T-150, T-151 (cv) |
| R3 — Mutation tenant-gating via WITH CHECK | T-134, T-135 (tracker), T-146, T-147 (companies) |
| R4 — NULLIF GUC hardening | T-117 (spike), T-118, T-119, T-120, T-121, T-122 |
| R5 — Behavior preservation | T-131..T-133 (scan), T-134..T-136 (tracker), T-137..T-139 (auth), T-140..T-142 (jobs), T-143..T-145 (evaluate), T-146..T-148 (companies), T-149..T-152 (cv) — every seam's verify task |
| R6 — Out of scope preserved (worker/queue/UpsertUser untouched, dead code removed) | T-125, T-126, T-137 (proof by omission), T-156 |
| Cross-seam regression safety | T-128, T-130, T-133, T-136, T-139, T-142, T-145, T-148, T-152, T-153, T-154, T-155 |

## Next step

Run `sdd-apply` with the resolved `delivery_strategy` (currently `ask-on-risk` per orchestrator context) and, once a chain strategy is confirmed by the user, implement seams in dependency order: Seam 0 → Seam 1 → Seams 2-8 (any order, parallelizable) → Seam 9.
