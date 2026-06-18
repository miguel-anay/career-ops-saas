# Proposal — `rls-tenancy-wiring`

> Phase: propose · Status: complete · Artifact store: openspec
> Make the Go API actually engage the RLS tenant context it was designed for — closing a live, MVP-blocking multi-tenant isolation gap.

## Intent — what problem, why now

The Go API connects to Postgres as `app_user`, a role that is **subject to** `FORCE ROW LEVEL SECURITY` on all 9 tenant tables. But the API **never sets** `app.current_user_id` on its query connections. `platform.WithTenant` has zero call sites, `middleware.TenantIsolation` is a literal no-op, and every domain service builds `*db.Queries` over a raw pooled connection. The only correct path is `cv.EnqueueIngest`/`cv.GetIngestion` (the just-merged ingest-cv `withTenant` helper).

Consequences, verified against code (see `explore.md`):

- **Tenant isolation is effectively unenforced API-wide.** Every RLS-gated query runs without a tenant GUC. Depending on pooled-connection reuse state, these deny silently, violate `WITH CHECK`, or throw `22P02` (`''::uuid`) → intermittent 500s.
- **Two paths are exploitable today, regardless of RLS state:**
  - `scan.GetScanRun` → `GetScanRunByID` filters on `WHERE id=$1` only; the handler discards `userID` and the service has no ownership check. This is a **live cross-tenant IDOR**.
  - `tracker.UpdateApplication` runs its ownership check **after** the mutating UPDATE — the app-layer guard is dead-for-security; only RLS `WITH CHECK` would stop a cross-tenant write, and RLS is not engaged.

This is a correctness and security gap, not a feature. It blocks MVP: a multi-tenant product whose tenant boundary is not enforced cannot ship. Now is the moment because the `ingest-cv` change just established the exact pattern (`withTenant`) and the DB-gated test template to generalize from.

This change is **behavior-preserving for legitimate same-tenant requests**: it makes the *already-intended* isolation actually work. No new product behavior.

## Scope

### In scope (API-only)

- **Engage RLS on every tenant-touching service method** by routing its sqlc calls through a shared tenant-tx helper that sets `app.current_user_id`:
  - `auth.GetUserByID` (refresh-token read — currently 100% RLS-reliant, likely broken today)
  - `jobs` (`AddManual`, `List`, `GetByID`)
  - `scan` (`TriggerScan`, **`GetScanRun`** — see below)
  - `evaluate` (`EnqueueEvaluation`, `GetReport`)
  - `companies` (`List`, `Add`, `Remove` — `DeleteWatchedCompany` is RLS-only)
  - `tracker` (`UpdateApplication` — plus the ordering fix)
  - the remaining `cv` methods not yet on `withTenant`: `EnqueuePDFGeneration`, `GetDownloadURL`, `ListCVs`, `CreateCV`, `SetMasterCV`
- **DB migration** hardening all 9 RLS policies from `current_setting('app.current_user_id', true)::uuid` to `NULLIF(current_setting('app.current_user_id', true), '')::uuid` (the empty-string `22P02` fix).
- **Fold in the two exploitable bugs** (both fixed for free by the same wiring):
  - `scan.GetScanRun` IDOR → fixed once the lookup runs under the tenant tx (non-owner sees zero rows → `ErrNotFound`/404). Highest-priority slice.
  - `tracker.UpdateApplication` ordering → ownership decided by RLS at the UPDATE, app check no longer the sole guard.
- **Per-domain DB-gated integration tests** following the `cv/ingest_integration_test.go` two-pool template (`app_user` exercises the service, superuser seeds + asserts ground truth), proving cross-tenant denial as `app_user`.

### Out of scope

- **Node worker** — already correct via `tenantQuery` in `worker/lib/db.mjs`; it is the reference pattern, not a target.
- **pg-boss / `queue.Enqueue`** — writes to the `pgboss` schema, which has no RLS policy. Keeps the raw pool, untouched.
- **`auth.UpsertUser`** — `auth_upsert_user` is `SECURITY DEFINER` by design (OAuth signup before a tenant context exists). Unchanged.
- Any new product behavior, new endpoints, or query/schema changes beyond the policy hardening.
- A pgx-native rewrite of `api/internal/db` (rejected — see Approach).

## Approach — recommended

**Approach 1: generalize the `cv.withTenant` tx helper into a shared per-call tenant helper that every service uses.** Each tenant-touching method switches from `s.queries()` (raw pool) to a `withTenant(ctx, userID, func(q *db.Queries) error { ... })` wrapper that opens a tx, runs `SELECT set_config('app.current_user_id', $1, true)`, executes the sqlc calls through `db.New(tx)`, and commits on success / rolls back on error. `queue.Enqueue` stays on the raw pool (separate schema). The exact placement of the shared helper (per-service vs. a `platform`/shared package, e.g. reviving `platform.WithTenant`) is a **design-phase** decision; this proposal fixes only the *pattern*, not the file layout.

### Why approach 1

- **Matches the already-merged precedent.** `cv.withTenant` is in `main` and proven by a live-DB test. We generalize a known-good pattern rather than invent one.
- **Converges on the worker's reference design.** `tenantQuery` (BEGIN / SET LOCAL / COMMIT) is structurally identical — API and worker end up using the same tenancy mechanic.
- **Smallest architectural risk.** No regeneration of `api/internal/db`, no change to `db.Queries`/`DBTX`, no new request-lifecycle plumbing. The tx boundary stays at the method level, where it already is for `cv`.
- **Parallelizable per domain.** Each service is an independent slice with an independent integration test — ideal for chained/stacked PRs.
- **`queue` untouched.** The raw-pool enqueue path keeps working with no special-casing.

Trade-off accepted: single-statement SELECTs now run inside a BEGIN/COMMIT tx (minor per-call overhead). For an MVP control plane this is negligible and is the price of correct isolation.

### Why not approach 2 (middleware-established per-request tenant tx via context)

A tenant tx that spans the whole request holds a pooled connection for the entire request lifetime — **including non-DB work** like `cv.GetDownloadURL`'s R2 signing call — creating a real **connection-pool-exhaustion risk** under load. It also still rewrites every service to pull the tx from context, so it does **not** reduce service-layer scope; it only moves where the tx begins, while adding long-lived-connection risk and context plumbing. Higher risk, no scope saving.

### Why not approach 3 (pgx-native rewrite)

Dropping the `database/sql` shim for sqlc pgx mode means **regenerating all of `api/internal/db`**, changing `db.Queries`/`DBTX`, and touching every service — the **largest blast radius and highest regression risk** of the three. The consistency upside (aligning with the already-pgx-native `queue`) is real but is a separate refactor, not justified by this security fix. Reject for this change; revisit independently later if desired.

## The NULLIF policy hardening — prerequisite slice

`current_setting('app.current_user_id', true)::uuid` returns NULL only when the GUC was *never* set on a session. Once a tenant tx runs on a **pooled physical connection** and ends, Postgres resets the custom GUC to `''` (empty string), not undefined — so a later non-tenant query on the same connection hits `''::uuid` → **`22P02`** → a 500 instead of a clean deny. All 9 policies use the bare unguarded form.

**Decision: the migration hardening every policy to `NULLIF(current_setting('app.current_user_id', true), '')::uuid` ships FIRST, as an atomic prerequisite slice (its own PR), landing before the service wiring.** It is required for correctness under connection pooling **regardless of which engagement approach is chosen** — even partially-wired states (a tenant tx ran, then a not-yet-migrated raw query reuses the connection) are exactly when the `22P02` footgun fires. Landing it first means every subsequent service slice merges onto a DB where empty-string GUC degrades to clean denial, not an error.

## Success criteria (measurable)

1. **One DB-gated integration test per domain** (`auth`, `jobs`, `scan`, `evaluate`, `companies`, `tracker`, remaining `cv`) following the two-pool template, each proving a non-owner request is denied (zero rows / `ErrNotFound` / `WITH CHECK` violation) while the owner succeeds — exercised as `app_user`, not a superuser.
2. **`scan.GetScanRun` returns 404 (not another tenant's row)** for a cross-tenant lookup, proven by an integration test.
3. **No mutation relies on an app-layer ownership check as its sole guard** — every tenant write is covered by RLS `WITH CHECK` under the tenant tx; `tracker.UpdateApplication`'s check no longer runs after an unscoped UPDATE.
4. **`make test-rls` (pgTAP) stays green** after the NULLIF migration — policies still enforce isolation; empty-string GUC degrades to denial, no `22P02`.
5. **No service method touches a tenant table over the raw pool** (raw pool reserved for `queue.Enqueue` only).
6. **Same-tenant behavior unchanged** — existing handler/service tests for legitimate owner requests still pass.

## Risks & mitigations

| Risk | Mitigation |
|---|---|
| **Live behavior unverified** — whether unset GUC produces `22P02` vs. silent empty vs. partial is pool-state-dependent and not empirically reproduced (no DB during explore). | Run a **short live-DB spike early** (before/at design): stand up the careerops container, confirm the `22P02` failure mode and that the NULLIF migration converts it to clean denial. De-risks the prerequisite slice and the whole approach. |
| **Review-budget overflow** — 6+ service files, 1 migration, 6+ new integration tests will exceed the 400-line PR budget. | Plan **chained/stacked PRs**: PR 1 = NULLIF migration (prerequisite); PR 2 = `scan.GetScanRun` (highest-priority security slice); then one PR per remaining domain. Each slice is independently testable. |
| **Partially-wired window** — between slices, some domains are tenant-wrapped and others still raw on the same pool. | The NULLIF migration landing first makes the mixed state *safe* (empty-string GUC → deny, not error). Order slices so the migration is always already merged. |
| **`auth.GetUserByID` is easy to miss** — it's the refresh-token read, outside the `/api` prefix conventions, with no app filter and no `SECURITY DEFINER`. | Explicitly enumerated in scope; gets its own integration test for the refresh-token path. |
| **Test infra needs live Postgres in CI** — the entire existing Go suite is mock-based; only `cv` is DB-gated today. | Tests skip cleanly without `TEST_DATABASE_URL` (template already does this); `make test-rls` already assumes Docker. Design phase decides shared harness vs. per-domain duplication. |

## Notes

- **Behavior-preserving** for legitimate same-tenant requests — this change makes existing intended isolation actually execute; it does not alter any successful owner-path response.
- File-by-file HOW (shared-helper placement, per-domain test harness, migration numbering) is deferred to the **design** phase.

## Next

Ready for `sdd-spec` and `sdd-design` (parallelizable). Design should resolve: shared helper location, integration-test harness shape, migration file, and incorporate the live-DB spike findings.
