# Exploration — `rls-tenancy-wiring`

> Phase: explore · Status: complete · Artifact store: openspec
> RLS tenant isolation is never engaged at runtime in the Go API.

## Current State (verified against code, not assumed)

1. `app_user` (docker-compose.yml `DATABASE_URL`) is a plain `CREATE ROLE … LOGIN PASSWORD` (db/migrations/001_initial.sql:8-14) — no SUPERUSER, no BYPASSRLS, not table owner. It **is** subject to RLS / FORCE RLS.
2. All 9 tenant tables (`users, watched_companies, jobs, applications, reports, cvs, scan_runs, usage, cv_ingestions`) have `ENABLE` + `FORCE ROW LEVEL SECURITY` with identical policies: `USING/WITH CHECK (user_id = current_setting('app.current_user_id', true)::uuid)` (db/rls.sql, db/migrations/001_initial.sql).
3. `platform.WithTenant` (api/internal/platform/postgres.go) sets `SET LOCAL app.current_user_id` on an acquired pgxpool connection — but has **zero call sites** anywhere except its own definition and a stale comment in middleware/tenant.go.
4. `middleware.TenantIsolation` (api/internal/middleware/tenant.go, mounted in api/cmd/api/main.go:83 on all `/api` routes) is a literal no-op: it checks `userID` is in context (redundant with `Authenticator`), discards the pool (`_ = pool`), does nothing else.
5. Every domain service (`jobs`, `scan`, `evaluate`, `companies`, `tracker`, most of `cv`) builds `*db.Queries` via `stdlib.OpenDBFromPool(pool)` raw connections through a `queries()` helper that never sets the GUC.
6. The ONLY code in `api/` that sets `app.current_user_id` is `cv.Service.withTenant` (api/internal/cv/service.go:53-70), added by the just-merged ingest-cv change (Seam B), used by exactly two methods: `EnqueueIngest`, `GetIngestion`. The other `cv` methods still use the unwrapped path — the gap is not even fully closed within `cv`.
7. `auth_upsert_user` (db/migrations/001_initial.sql:203-219) is `SECURITY DEFINER`, runs without any tenant GUC — the only write path during OAuth login/signup. This is why login "works" today despite RLS being otherwise dead.
8. `auth.GetUserByID` (api/internal/auth/service.go:73) queries `users` via the raw pool with no tenant GUC and no SECURITY DEFINER wrapper — a refresh-token-flow read that, as `app_user` with the GUC unset, hits `current_setting(..., true)::uuid` → `''::uuid` → **22P02 error**. Needs live verification.

## Reality check — does the app function today?

Inconsistently, by path:
- **Login/signup**: works — `auth_upsert_user` SECURITY DEFINER bypasses RLS by design.
- **Refresh-token user lookup** (`auth.GetUserByID`): likely **errors** (22P02) under real DB load.
- **Everything else** (jobs, scan, evaluate, companies, tracker, most of cv): every RLS-gated SELECT/UPDATE/DELETE through the raw pool should produce zero rows (SELECT), a WITH CHECK violation (INSERT), or a 22P02 cast error — depending on whether a prior query on the same pooled physical connection left the GUC as `''`. **Live-DB-dependent; not empirically reproduced (no DB available) — top unknown for the proposal phase to verify via a quick spike.**
- **cv ingest path** (`EnqueueIngest`/`GetIngestion`): works correctly — RLS genuinely engaged via `withTenant`.

## Affected Areas — per-domain service map

| Service | Methods | Filter type | Breaks once RLS truly engages? |
|---|---|---|---|
| `auth` | `UpsertUser` (`auth_upsert_user` SECURITY DEFINER) | bypasses RLS by design | No |
| `auth` | `GetUserByID` (raw `SELECT … FROM users WHERE id=$1`) | **none** — 100% RLS-reliant | Yes — refresh-token flow errors/denies |
| `jobs` | `AddManual`→`UpsertJobByURL` | RLS WITH CHECK only | Yes |
| `jobs` | `List`→`ListJobsByUser` | app-layer `WHERE user_id=$1` | No (redundant once RLS engages) |
| `jobs` | `GetByID`→`GetJobByID` (`WHERE id=$1` only) | RLS-only + post-fetch ownership check | Redundant-but-safe once RLS engages |
| `scan` | `TriggerScan` (list app-filtered; InsertScanRun WITH CHECK) | partial | INSERT needs correct GUC |
| `scan` | **`GetScanRun`→`GetScanRunByID`** (`WHERE id=$1`, **no ownership check**; handler discards `userID`) | **zero filtering except RLS** | **CRITICAL — live cross-tenant IDOR today.** Highest-priority fix, independent of RLS wiring |
| `evaluate` | `EnqueueEvaluation`, `GetReport` | mix: usage app-filtered; by-ID lookups unfiltered + post-fetch check | Yes for unfiltered lookups |
| `companies` | `List`, `Add`, `Remove` | `DeleteWatchedCompany` has **no WHERE user_id** — RLS-only for the mutation | Yes |
| `tracker` | `UpdateApplication`→`UpdateApplicationStatus/Notes` (`WHERE id=$1`) | **ownership check runs AFTER the mutating UPDATE** | Yes — app check is dead-for-security; relies entirely on RLS WITH CHECK |
| `cv` | `EnqueueIngest`, `GetIngestion` (via `withTenant`) | real RLS | No — already correct (the template) |
| `cv` | `EnqueuePDFGeneration`, `GetDownloadURL`, `ListCVs`, `CreateCV`, `SetMasterCV` (plain `queries()`) | mix | Yes for the unfiltered by-ID lookups |
| `ws` | none — in-memory pub/sub | n/a | Not affected |
| `queue` | `Enqueue` → `pgboss.job` (separate schema, no RLS) | n/a | Not affected — can keep the raw pool |

**Pattern**: every "get/update/delete by ID" sqlc query has NO `user_id` in its WHERE clause — RLS was meant to be the only scoping. The Go-side `if row.UserID != userID { return ErrNotFound }` checks are secondary defense that only run AFTER the query executed unscoped. `scan.GetScanRun` and `tracker.UpdateApplication` are the sharpest examples of why dead RLS matters beyond hygiene — exploitable today.

## The cv `withTenant` precedent (template to generalize)

```go
func (s *Service) withTenant(ctx context.Context, userID uuid.UUID, fn func(q *db.Queries) error) error {
	sqlDB := stdlib.OpenDBFromPool(s.pool)
	tx, err := sqlDB.BeginTx(ctx, nil)
	...
	tx.ExecContext(ctx, "SELECT set_config('app.current_user_id', $1, true)", userID.String())
	fn(db.New(tx))
	return tx.Commit()
}
```
- Transaction-local `set_config(..., true)` (equiv `SET LOCAL`), scoped to the tx lifetime — not the raw pool.
- All sqlc calls inside `fn` go through `db.New(tx)`.
- Commits on success, rolls back on any error.
- Test template: `api/internal/cv/ingest_integration_test.go` — two real connections: `appPool` (`app_user`, RLS enforced) to exercise the Service, `adminPool` (superuser, creds swapped in DSN) to seed via `auth_upsert_user` and assert ground truth. Skips without `TEST_DATABASE_URL`. The only DB-gated Go integration test proving real RLS engagement.

## The empty-string GUC fragility (confirmed)

- `current_setting('app.current_user_id', true)::uuid` returns NULL only when the setting was NEVER set on that session. Once `set_config(..., true)` runs on a **pooled physical connection** and the tx ends, Postgres resets the custom GUC to `''` (empty string), not undefined.
- Consequence: after ANY tenant tx runs on a pooled connection, a LATER query on the SAME physical connection that does NOT go through a tenant wrapper hits `''::uuid` → **22P02 error** instead of clean denial → a 500.
- Connection-pool-dependent footgun: mixing tenant-wrapped and non-wrapped paths against the same pool produces intermittent errors.
- All 9 policies use the bare `current_setting('app.current_user_id', true)::uuid` form — none guard against `''`.
- Fix candidate (not decided): harden every policy to `NULLIF(current_setting('app.current_user_id', true), '')::uuid` via a new migration. Independent of which engagement approach is chosen — needed either way under pooling.

## Worker comparison (reference design — confirms API-only scope)

`worker/lib/db.mjs` `tenantQuery(userId, sql, params)`:
```js
await client.query('BEGIN')
await client.query(`SET LOCAL app.current_user_id = $1`, [userId])
const result = await client.query(sql, params)
await client.query('COMMIT')
```
Structurally identical to `cv.withTenant`. The worker is already correct — the fix is **API-only**; the worker is the reference pattern the API should converge toward.

## Candidate Approaches (exploration only — no decision)

1. **Generalize `cv.withTenant` into a shared helper** every service calls.
   - Blast radius: every service file (~6 files, 20+ methods) switches from `s.queries()` to the tenant-tx pattern.
   - Testability: `cv/ingest_integration_test.go` is a ready template (two-pool), replicable per domain or via a shared harness.
   - pg-boss: `queue.Enqueue` keeps the raw pool (separate schema) — safe.
   - Implications: every method becomes a BEGIN/COMMIT tx even for single SELECTs (slightly higher overhead); eliminates the GUC fragility only when combined with the NULLIF fix.
   - Effort: Medium-High, mechanical, parallelizable per domain.
2. **Make `TenantIsolation` middleware establish a per-request tenant tx pulled from context.**
   - Blast radius: middleware + context helper, AND still rewrites every service to pull the tx from context — does not reduce service-layer scope, just moves where the tx begins.
   - Testability: harder; tx spans the whole request (incl. non-DB work like `cv.GetDownloadURL`'s R2 call) → connection-pool exhaustion risk.
   - Effort: Medium-High plus context-plumbing + long-lived-connection risk.
3. **Switch to pgx-native** (drop `stdlib.OpenDBFromPool`/database-sql shim; sqlc pgx mode + `pgx.Tx`).
   - Blast radius: largest — regenerate all of `api/internal/db/`, change `db.Queries`/`DBTX`, touch every service.
   - Upside: makes `queue` (already pgx-native) consistent with the rest; lower-overhead tx handling.
   - Effort: High, highest regression risk, most consistent end state.

## Test Strategy Implications

- The entire Go suite is mock-based; every `handler_test.go` mocks the `Servicer` — cannot prove RLS engagement.
- `auth/integration_test.go` and `jobs/integration_test.go`, despite the name, are httptest+mock, NOT DB-backed. Only `cv/ingest_integration_test.go` is genuinely DB-gated.
- Proving RLS engagement requires new DB-gated integration tests, one per domain (or a shared harness), following the cv two-pool pattern. Needs a live Postgres in CI (`make test-rls` already assumes Docker).
- pgTAP tests validate the DB layer in isolation (policies + FORCE flags), NOT that the Go code engages the GUC. Both layers needed.

## Top Unknowns / Risks for Proposal Phase

1. Unverified empirically: errors (22P02) vs. silent empty vs. partial, depending on pool-reuse state — needs a live-DB spike.
2. **`scan.GetScanRun`** is a live cross-tenant IDOR today — candidate P0 fix independent of/ahead of the broader change.
3. **`tracker.UpdateApplication`** ownership check runs AFTER the UPDATE — relies entirely on RLS WITH CHECK; app check is dead-for-security.
4. **`auth.GetUserByID`** (refresh-token flow) — no SECURITY DEFINER, no app filter — likely broken today; easy to miss (not under `/api` prefix conventions).
5. The NULLIF empty-string fix is a hard prerequisite for any approach under pooling — should land before or atomically with the chosen approach.
6. Approaches 1 and 2 both rewrite every service regardless — the real decision is WHERE the tx boundary lives.
7. Review-workload: 6+ service files, 1 migration, 6+ new integration tests → likely exceeds the 400-line budget; anticipate chained/stacked PRs.

## Ready for Proposal

Yes. The proposal phase should explicitly decide: (a) which of the 3 approaches to generalize; (b) whether the NULLIF migration ships atomically or as a prerequisite slice; (c) whether `scan.GetScanRun`'s IDOR and `tracker.UpdateApplication`'s ordering are fixed in this change or spun out as an urgent standalone fix.
