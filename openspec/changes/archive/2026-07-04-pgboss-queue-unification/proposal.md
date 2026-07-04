# Proposal: pg-boss Queue Unification (v10 on both services)

## Intent

The Go API and Node worker target **incompatible queue schemas**. Go's `queue.Enqueue` does a raw `INSERT` against a hand-rolled flat `pgboss.job` table; the worker runs **pg-boss v10.4.2**, which expects a **partitioned** `pgboss.job` plus a `pgboss.queue` registry. The two sides were never integration-tested together. Result: 4135 jobs enqueued by Go that the worker can never consume, and pg-boss v10's `insertJob` returns `null` (no exception) when a queue is unregistered — a silent-failure trap. We need one queue contract, end-to-end verified.

## Scope

### In Scope
- Rewrite `api/internal/queue/boss.go` `Enqueue` to match pg-boss v10's partitioned `insertJob` contract; **fail loudly** — check `RETURNING id`, treat zero rows as an explicit error.
- Pin `pg-boss` to an **exact** version (drop `^10.0.0`) in `worker/package.json`.
- Admin-owned out-of-band queue registration: `createQueue` for all 4 names (`scan-company`, `evaluate-job`, `generate-pdf`, `ingest-cv`) via `worker/scripts/install-pgboss.mjs` (or sibling) + `db/pgboss_grants.sql`. Never at runtime as `app_user`.
- Rewrite test fixtures `api/internal/testsupport/rlsdb/harness.go` (`EnsurePgbossStandin`) and `api/internal/cv/ingest_integration_test.go` to the real v10 schema (consider `PgBoss.getConstructionPlans('pgboss')` to generate DDL).
- **New cross-service integration test**: Go `Enqueue` → worker dequeue against the real v10 schema (the missing acceptance test).
- Discard stuck jobs: `ALTER TABLE pgboss.job RENAME TO pgboss_job_orphaned_pre_v10` for forensics, then drop/replace before the real v10 install. No row migration.

### Out of Scope
- Outbox/relay pattern (Option 3) — cleaner long-term, deferred as noted future work.
- Selective re-enqueue of the 4135 orphaned rows.
- pg-boss v9 (unmaintained on npm — rejected).

## Capabilities

### New Capabilities
- `queue-enqueue-contract`: Go-side enqueue must satisfy pg-boss v10's registered-queue partitioned insert and error on unregistered queues.
- `queue-provisioning`: admin-owned, out-of-band install of the pgboss schema + queue registration.

### Modified Capabilities
- None (no existing `openspec/specs/` capability — first spec for the queue path).

## Approach

Adopt **full pg-boss v10 on both services** (locked). Go cannot use the Node library, so `Enqueue` hand-replicates v10's `insertJob` SQL against the partitioned table on the raw `pgxpool.Pool` (never `WithTenantTx` — `pgboss.*` has no RLS). The admin installs the schema with `migrate:true` and registers all 4 queues; the worker boots with `migrate:false`. Two mandatory mitigations bound the risk: exact version pin + a cross-service contract/integration test gating CI. Call sites (`scan/service.go:74`, `evaluate/service.go:101`, `cv/service.go:102` & `:290`) keep their signature — behavior-preserving for the rest of the system.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `api/internal/queue/boss.go` | Modified | Rewrite `Enqueue` to v10 contract; loud error on zero `RETURNING` rows |
| `worker/package.json` | Modified | Exact version pin |
| `worker/scripts/install-pgboss.mjs`, `db/pgboss_grants.sql` | Modified/New | Admin install + `createQueue` for 4 names |
| `api/internal/testsupport/rlsdb/harness.go` | Modified | Real v10 fixture DDL |
| `api/internal/cv/ingest_integration_test.go` | Modified | Fixture wrapper update |
| cross-service integration test | New | enqueue → dequeue acceptance test |
| `pgboss.job` (4135 rows) | Removed | Rename to orphaned, drop before install |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Cross-service contract drift on pg-boss version bump | High | Exact version pin + CI contract test |
| Unregistered-queue silent failure | Med | Loud `RETURNING id` error; admin pre-registers all names |
| Default-privileges gap if `createQueue` runs as `app_user` | Med | Registration stays admin-only, out-of-band |
| Test fixture duplicates real schema shape | Med | Generate DDL from `getConstructionPlans` |

## Rollback Plan

Revert the Go `Enqueue` and fixture commits; restore `^10.0.0` pin. The orphaned table is renamed (not dropped) for forensics, so the prior state is recoverable until the deliberate drop step. No data is migrated, so rollback loses nothing new.

## Dependencies

- Admin/owner DB connection string to install the pgboss schema and register queues (separate from `app_user`).

## Success Criteria

- [ ] End-to-end enqueue (Go) → dequeue (worker) passes against the real v10 schema.
- [ ] `queue.Enqueue` returns an explicit error when the target queue is unregistered.
- [ ] `make test-all` is green (strict TDD active).
- [ ] All 4 queue names registered out-of-band by admin; worker boots `migrate:false`.
- [ ] Orphaned table renamed for forensics; no v10 install over stale rows.
