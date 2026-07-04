# Spec — `rls-tenancy-wiring`

> Phase: spec · Status: complete · Artifact store: openspec
> Input: `openspec/changes/rls-tenancy-wiring/proposal.md` (Approach 1 — generalized tenant-tx helper), `openspec/changes/rls-tenancy-wiring/explore.md` (per-domain map, GUC fragility, cv precedent)

This is a delta spec: it defines what MUST be true after `rls-tenancy-wiring` ships. The change is **API-only** and **behavior-preserving** for legitimate same-tenant requests — it makes tenant isolation that was already *intended* actually enforced at the database layer, in addition to the existing app-layer checks. No new product behavior, no new endpoints.

**Current-state note**: `scan.GetScanRun`'s cross-tenant IDOR was already hotfixed and merged to `main` (PR #8, `api/internal/scan/service.go`) — the handler passes `userID` to the service, and the service returns `sql.ErrNoRows` for a non-owned scan run via an app-layer `scanRun.UserID != userID` check. That app-layer check is explicitly documented in the code as defense-in-depth, pending the DB-level RLS gate this change wires. This spec does **not** treat the IDOR as undiscovered; it treats `scan` as a domain needing RLS engagement like the others, with the added requirement that the existing app-layer guard keep working as a second line of defense.

## Requirement 1 — Every tenant-touching service query engages RLS via a shared tenant-tx helper

Every Go API service method that reads, writes, or enqueues against a tenant table (`users`, `watched_companies`, `jobs`, `applications`, `reports`, `cvs`, `scan_runs`, `usage`, `cv_ingestions`) MUST run its sqlc calls inside a transaction that sets `app.current_user_id` to the requesting user's ID, using the generalized form of the `cv.withTenant` helper (`api/internal/cv/service.go`). No service method may build `*db.Queries` over the raw pool for a tenant-table query (the raw pool is reserved for `queue.Enqueue`, which targets the non-RLS `pgboss` schema).

#### Scenario: GUC-scoped query returns only the matching tenant's rows
- **Given** a live Postgres connection authenticated as `app_user` (not superuser)
- **And** rows exist in a tenant table for both user `A` and user `B`
- **When** a query runs inside a transaction with `app.current_user_id` set to `A`
- **Then** the query returns only `A`'s rows
- **And** when the same query runs inside a transaction with `app.current_user_id` set to `B`, it returns only `B`'s rows (zero of `A`'s rows)

#### Scenario: No tenant-table service method bypasses the tenant-tx helper
- **Given** the full set of tenant-touching methods across `auth`, `jobs`, `scan`, `evaluate`, `companies`, `tracker`, and `cv`
- **When** each method's data-access path is inspected
- **Then** every one of them calls the shared tenant-tx helper (not `s.queries()` / a raw-pool `*db.Queries`) for any statement touching a tenant table
- **And** `queue.Enqueue` is the only call site still using the raw pool, and it never touches a tenant table

## Requirement 2 — Cross-tenant reads are denied at the DB layer, per domain

For each affected domain (`auth`, `jobs`, `scan`, `evaluate`, `companies`, `tracker`, `cv`), a request by user `B` for a resource owned by user `A` MUST be denied by Postgres RLS when exercised through a live `app_user` connection — not merely by an app-layer `if owner != caller` check running after an unscoped query.

#### Scenario: auth.GetUserByID denies cross-tenant read (refresh-token path)
- **Given** two users `A` and `B` exist, exercised via the `app_user` integration pool
- **When** `auth.Service.GetUserByID` runs under a tenant tx scoped to `B` but is asked to look up `A`'s user ID
- **Then** the query returns no row (RLS `USING` clause excludes it), and the service surfaces this as a not-found/error condition rather than `A`'s user record

#### Scenario: jobs.GetByID denies cross-tenant read
- **Given** a `jobs` row owned by user `A`
- **When** user `B` calls the equivalent of `jobs.Service.GetByID` for that job's ID, exercised against a live `app_user` connection scoped to `B`
- **Then** the underlying `GetJobByID` query returns zero rows
- **And** the handler-level response is `404 Not Found`, proven by a DB-gated integration test (not a mock)

#### Scenario: scan.GetScanRun is denied at the DB layer in addition to the existing app-layer check
- **Given** a `scan_runs` row owned by user `A` (the already-hotfixed app-layer ownership check in `scan.Service.GetScanRun` remains in place)
- **When** user `B` calls `GET /api/scan-runs/{id}` for `A`'s scan run, exercised against a live `app_user` connection scoped to `B`
- **Then** `GetScanRunByID`, now running inside a tenant tx scoped to `B`, returns zero rows from Postgres itself (RLS denial), independent of the app-layer `scanRun.UserID != userID` check
- **And** the handler response is `404 Not Found`
- **And** removing the app-layer check (hypothetically) would still result in a 404, because RLS is now the binding constraint — proven by a DB-gated integration test asserting the query result set is empty under `B`'s tenant tx

#### Scenario: evaluate.GetReport denies cross-tenant read
- **Given** a `reports` row owned by user `A`
- **When** user `B` requests that report's ID through `evaluate.Service.GetReport`, exercised against a live `app_user` connection scoped to `B`
- **Then** the underlying lookup returns zero rows and the service/handler responds as not-found

#### Scenario: companies.List/Add scoped correctly, no cross-tenant leakage
- **Given** `watched_companies` rows owned by user `A` and user `B`
- **When** user `B` calls `companies.Service.List`, exercised against a live `app_user` connection scoped to `B`
- **Then** only `B`'s watched companies are returned, never `A`'s

#### Scenario: cv non-ingest methods deny cross-tenant read
- **Given** a `cvs` row owned by user `A`
- **When** user `B` calls `cv.Service.GetDownloadURL` (or `ListCVs`) for that CV's ID, exercised against a live `app_user` connection scoped to `B`
- **Then** the underlying query returns zero rows and the service/handler responds as not-found, matching the already-correct behavior of `cv.EnqueueIngest`/`cv.GetIngestion`

## Requirement 3 — Mutations are tenant-gated at the DB (WITH CHECK), not just app-layer

UPDATE and DELETE statements that target a row by ID (`tracker.UpdateApplication`, `companies.Remove`) MUST be unable to affect another tenant's row even if the app-layer ownership check were absent, because the operation runs inside a tenant tx and Postgres RLS `WITH CHECK` (and `USING`, for the row the UPDATE/DELETE targets) enforces `user_id = current_setting('app.current_user_id', true)::uuid`.

#### Scenario: tracker.UpdateApplication cannot mutate another tenant's row
- **Given** an `applications` row owned by user `A`
- **When** user `B` calls `tracker.Service.UpdateApplication` for that row's ID, exercised against a live `app_user` connection scoped to `B`
- **Then** the UPDATE affects zero rows (RLS `USING` excludes `A`'s row from `B`'s tenant tx — the row is invisible to the UPDATE's target scan)
- **And** the ownership check in `tracker.Service` (if still present) runs against this RLS-backed result, not before an unscoped UPDATE already mutated the row
- **And** `A`'s row is verified unchanged via the superuser/ground-truth pool after the attempt

#### Scenario: companies.Remove cannot delete another tenant's row
- **Given** a `watched_companies` row owned by user `A` (`DeleteWatchedCompany` has no `WHERE user_id` clause in the sqlc query itself — RLS is the only scoping mechanism)
- **When** user `B` calls `companies.Service.Remove` for that row's ID, exercised against a live `app_user` connection scoped to `B`
- **Then** the DELETE affects zero rows
- **And** `A`'s row is verified still present via the superuser/ground-truth pool after the attempt

#### Scenario: Owner mutation still succeeds
- **Given** an `applications` row owned by user `A`
- **When** user `A` calls `tracker.Service.UpdateApplication` for that row's own ID, exercised against a live `app_user` connection scoped to `A`
- **Then** the UPDATE affects exactly one row and the change is visible on a subsequent read scoped to `A`

## Requirement 4 — Empty-string GUC hardening: clean denial, not a cast error

All 9 RLS policies (`db/rls.sql`) MUST tolerate a session where `app.current_user_id` was previously set on a pooled physical connection and has since reset to Postgres's empty-string default, by denying access cleanly — not raising a `22P02` invalid-input-syntax error. This requires migrating every policy's `current_setting('app.current_user_id', true)::uuid` to `NULLIF(current_setting('app.current_user_id', true), '')::uuid`.

#### Scenario: Empty-string GUC denies cleanly instead of erroring
- **Given** a pooled `app_user` physical connection on which `app.current_user_id` was previously set via `set_config(..., true)` inside a now-completed transaction (so the GUC has reset to `''`, Postgres's default for a custom setting that was set-local and the tx ended)
- **When** a query against any of the 9 RLS-protected tables runs on that connection with the GUC at `''` (no new tenant tx wrapping it)
- **Then** the query returns zero rows (RLS denial), not a `22P02 invalid input syntax for type uuid` error

#### Scenario: NULLIF migration does not change behavior for a properly-set GUC
- **Given** a tenant tx that sets `app.current_user_id` to a valid user UUID
- **When** a query runs inside that tx against an RLS-protected table
- **Then** rows matching that user are returned exactly as before the NULLIF migration (no regression for the happy path)

#### Scenario: pgTAP RLS tests stay green after the NULLIF migration
- **Given** the existing pgTAP RLS test suite (`make test-rls`)
- **When** the NULLIF migration is applied
- **Then** all existing pgTAP assertions (per-table cross-tenant invisibility, FORCE RLS flags) continue to pass with no test changes required

#### Scenario: NULLIF migration ships before any service is wired to the tenant-tx helper
- **Given** the migration ordering decided in the proposal (NULLIF hardening lands first, as its own prerequisite slice)
- **When** any service slice is merged that begins using the tenant-tx helper
- **Then** the NULLIF migration is already applied in that environment, so a partially-wired window (some domains tenant-wrapped, others still raw on the same pool) degrades to clean denial rather than intermittent `22P02` errors

## Requirement 5 — Legitimate same-tenant behavior is unchanged

Every existing successful same-tenant code path MUST continue to behave identically after RLS engagement — this change adds enforcement, it does not alter any successful owner-path request or response shape.

#### Scenario: jobs.List returns the caller's own jobs
- **Given** an authenticated user `A` with jobs in the `jobs` table
- **When** `A` calls `GET` for their job list
- **Then** the response is unchanged from pre-change behavior: all of `A`'s jobs, none of any other tenant's

#### Scenario: scan.TriggerScan still enqueues and inserts correctly
- **Given** an authenticated user `A` with enabled watched companies
- **When** `A` calls `POST /api/scan`
- **Then** a `scan_runs` row is inserted for `A`, one `scan-company` job is enqueued per enabled company, and the response is `202 Accepted` with the `scan_run_id` — unchanged from pre-change behavior

#### Scenario: evaluate.EnqueueEvaluation still succeeds for the owner
- **Given** an authenticated user `A` within usage limits
- **When** `A` triggers an evaluation
- **Then** the evaluation is enqueued and the response is unchanged from pre-change behavior

#### Scenario: cv.CreateCV / ListCVs still succeed for the owner
- **Given** an authenticated user `A`
- **When** `A` creates a CV or lists their CVs
- **Then** the create succeeds and the list returns exactly `A`'s CVs, unchanged from pre-change behavior

#### Scenario: cv.EnqueuePDFGeneration still succeeds for the owner
- **Given** an authenticated user `A` with an existing CV
- **When** `A` requests PDF generation for their own CV
- **Then** the job is enqueued and the response is unchanged from pre-change behavior

#### Scenario: tracker.UpdateApplication still succeeds for the owner
- **Given** an `applications` row owned by user `A`
- **When** `A` updates their own application's status/notes
- **Then** the update succeeds and is visible on a subsequent read, unchanged from pre-change behavior

#### Scenario: Existing mock-based handler/service tests still pass
- **Given** the existing Go test suite (`handler_test.go` files across `jobs`, `scan`, `evaluate`, `companies`, `tracker`, `cv`, `auth`)
- **When** the tenant-tx helper is wired in
- **Then** these tests pass unmodified or with only mechanical updates to mock expectations (no behavioral assertions change), because `Servicer` interfaces and handler-level contracts are unchanged

## Requirement 6 — Out of scope is explicitly preserved, not incidentally changed

The following MUST remain unmodified by this change, to bound its blast radius:

#### Scenario: Node worker is untouched
- **Given** `worker/lib/db.mjs`'s `tenantQuery` helper (BEGIN / SET LOCAL / COMMIT)
- **When** this change ships
- **Then** no file under `worker/` is modified — it is already correct and serves as the reference pattern, not a target

#### Scenario: pg-boss / queue schema is untouched
- **Given** `queue.Enqueue` writing to the `pgboss` schema (no RLS policy exists there)
- **When** this change ships
- **Then** `api/internal/queue` keeps using the raw pool unchanged — no tenant-tx wrapping is applied to pg-boss job enqueueing

#### Scenario: auth.UpsertUser is untouched
- **Given** `auth_upsert_user` is `SECURITY DEFINER` by design (runs during OAuth signup/login, before any tenant context exists)
- **When** this change ships
- **Then** `auth.Service.UpsertUser` and the underlying `auth_upsert_user` function are not modified — login/signup behavior is identical

#### Scenario: No new product behavior or endpoints
- **Given** the full set of HTTP routes registered across all domains
- **When** this change ships
- **Then** no route is added, removed, or renamed, and no response shape changes for any successful request — the only observable difference is that previously-unenforced cross-tenant requests now correctly fail

## Traceability

| Requirement | Domains / files |
|---|---|
| R1 (shared tenant-tx helper, no raw-pool tenant queries) | `api/internal/platform/postgres.go` (or generalized helper location, design-phase decision), `api/internal/auth/service.go`, `api/internal/jobs/service.go`, `api/internal/scan/service.go`, `api/internal/evaluate/service.go`, `api/internal/companies/service.go`, `api/internal/tracker/service.go`, `api/internal/cv/service.go` |
| R2 (cross-tenant read denial per domain) | `auth.GetUserByID`, `jobs.GetByID`, `scan.GetScanRun` (`api/internal/scan/service.go`, `api/internal/scan/handler.go` — app-layer check already merged in PR #8), `evaluate.GetReport`, `companies.List`, `cv.GetDownloadURL`/`ListCVs` |
| R3 (mutation tenant-gating via WITH CHECK) | `tracker.UpdateApplication` (`api/internal/tracker/service.go`), `companies.Remove`/`DeleteWatchedCompany` (`api/internal/companies/service.go`) |
| R4 (NULLIF GUC hardening) | `db/rls.sql`, new migration under `db/migrations/`, `make test-rls` pgTAP suite |
| R5 (behavior preservation) | All domains listed above; existing `handler_test.go` files |
| R6 (out of scope) | `worker/lib/db.mjs`, `api/internal/queue`, `auth.UpsertUser` / `auth_upsert_user` |

## Cross-cutting: tenant isolation verification method

Every scenario in Requirements 2 and 3 MUST be proven by a DB-gated integration test exercised as `app_user` (not a superuser, not a mock) against a live Postgres instance, following the two-pool template established by `api/internal/cv/ingest_integration_test.go`: one pool authenticated as `app_user` to exercise the service under test, one superuser/ground-truth pool to seed fixtures and assert the true row state independent of RLS. Mock-based `handler_test.go` assertions are insufficient to satisfy these requirements — they prove handler wiring, not DB-layer enforcement.
