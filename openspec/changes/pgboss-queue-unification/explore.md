# Exploration: pgboss-queue-unification

> Mirror of engram `sdd/pgboss-queue-unification/explore` (hybrid artifact store).

## Current State

Confirmed by reading source directly:

1. **Go side** (`api/internal/queue/boss.go`): `Enqueue()` does a raw `INSERT INTO pgboss.job (id, name, data, state, "createdOn", "startAfter", "expireIn", priority) VALUES ($1,$2,$3,'created',$4,$4,interval '15 minutes',0)`. Called from 4 sites: `api/internal/scan/service.go:74` (`scan-company`), `api/internal/evaluate/service.go:101` (`evaluate-job`), `api/internal/cv/service.go:102` (`generate-pdf`) and `:290` (`ingest-cv`).

2. **Node side**: `worker/package.json` pins `"pg-boss": "^10.0.0"`; installed resolves to **10.4.2**. `worker/lib/queue.mjs` constructs `new PgBoss({connectionString, schema:'pgboss', migrate:false})`. `worker/index.mjs` registers 4 handlers via `boss.work()`: scan-company, evaluate-job, generate-pdf, ingest-cv.

3. **pg-boss v10.4.2 schema** (schema version **24**): creates `pgboss.version`, `pgboss.queue` (registry), `pgboss.schedule`, `pgboss.subscription`, `pgboss.job` (**`PARTITION BY LIST (name)`**, snake_case cols + `pgboss.job_state` enum), `pgboss.archive`, plus PL/pgSQL `create_queue(name, options)` / `delete_queue(name)` that dynamically create per-queue partition tables.
   - **Queue registration is mandatory and NOT automatic.** Only `manager.js createQueue()` inserts into `pgboss.queue` / creates the partition. `work()` and `send()`/`createJob()` never call it.
   - `insertJob()` JOINs `pgboss.queue`; an unregistered name → zero rows, **no exception** (`createJob()` returns `null`). A raw INSERT against the partitioned table with an unregistered name raises a partition-routing error, not a clean app error.
   - `contractor.check()` (used when `migrate:false`) validates only `isInstalled()` + `schemaVersion()===24`. It does NOT check registered queues → missing-queue failure surfaces later, per-job, silently.

4. **v9 line**: single shared non-partitioned `pgboss.job`, camelCase cols (closer to the hand-rolled table and Go `Enqueue`). No `queue` table, no `createQueue()`, no JOIN in `insertJob()`.

5. **npm maintenance**: only two maintained dist-tags — `maint-v10` (10.4.2, installed) and `latest` (12.x). **No `v9` dist-tag.** v9 is a frozen, unmaintained line.

6. **RLS / pool separation** (`api/internal/platform/postgres.go:34-46`): `pgboss.*` has no RLS; queue writes stay on the raw `pgxpool.Pool`, never `WithTenantTx`. Orthogonal to version choice; already respected by `queue.Enqueue`.

7. **Provisioning direction already drafted**: `worker/scripts/install-pgboss.mjs` (admin install, `migrate:true`, then stop) + `db/pgboss_grants.sql` (app_user USAGE + DML + `ALTER DEFAULT PRIVILEGES`). `app_user` has no CREATE (`db/migrations/001_initial.sql`).
   - **Gap**: `ALTER DEFAULT PRIVILEGES ... ON TABLES` only fires for tables later created **by the admin role**. If `app_user` ever calls `create_queue()` at runtime, the partition table it creates is owned by `app_user`, the default-privileges grant won't apply. → Queue registration MUST be an admin-side out-of-band step, never the worker at runtime.

8. **4135 stuck rows** in the hand-rolled flat table — incompatible with the real v10 partitioned schema (state is `'created'` text not the enum; no partition exists to route into; no v10 defaults applied).

9. **Test coverage today**: `api/internal/testsupport/rlsdb/harness.go` (`EnsurePgbossStandin`) and `api/internal/cv/ingest_integration_test.go` self-create the SAME flat fake table as a fixture; scan/evaluate RLS tests reuse the harness. Worker `tests/index.test.mjs` mocks `lib/queue.mjs`. **No test anywhere verifies an end-to-end enqueue→dequeue against the REAL pg-boss schema** — the actual root cause that let this ship.

## Approaches

### 1. Full pg-boss v10 (registered queues + partitioned schema)
- Go `Enqueue()` must hand-replicate v10's snake_case partitioned-table INSERT contract (replicating `insertJob()` defaulting in Go). Largest fragility: a pg-boss patch bump can silently desync the Go INSERT from the Node consumer.
- Worker: register all 4 queues via `createQueue()` **admin-side**, once, before any enqueue. `migrate:false` stays correct.
- Pros: maintained line, richer features, partitioned scaling, zero npm change. Cons: version-coupled Go SQL contract; silent missing-queue trap; both test fixtures rewritten. **Effort: High.**

### 2. Pin BOTH sides to pg-boss v9.x
- Go INSERT much closer to today's; v9 has no queue table / JOIN. Stuck rows migratable via column mapping.
- Pros: lowest-risk Go rewrite, no silent trap, fixtures need only column updates, backlog migratable. Cons: **v9 unmaintained on npm**; locks out v10+ features; downgrading installed v10.4.2 is a step backward needing justification. **Effort: Medium.**

### 3. Outbox: Go writes `public.outbox_job`, a Node relay drains it via real `boss.send()`
- Go never touches `pgboss.*`; only Node (via the real library) does. Future pg-boss upgrades = worker-only change.
- Pros: structurally eliminates the root cause; no Go↔pg-boss SQL coupling. Cons: new relay component + latency + retention; over-engineered for two services; adds a second queue-like construct (contradicts ADR-1 "no extra broker"). **Effort: Medium-High.**

## Recommendation

**Option 1 (full pg-boss v10, registered queues, admin-provisioned)** — gated on two mitigations baked in before apply:

1. Queue registration (`createQueue()` for all 4 names) runs as part of the admin-owned install step, never a runtime `app_user` path. Closes the default-privileges gap and the enqueue-before-queue race.
2. Go `queue.Enqueue` fails loudly: check `RETURNING id`, treat zero rows as an explicit error (converts pg-boss's silent-failure into a visible Go error).

Justification: v9 (option 2) is unmaintained (npm dist-tags confirm only `maint-v10` + `latest`); picking it trades a one-time cost for an open-ended unpatched-dependency liability, on a project already burned once by a queue-schema mismatch. The worker already runs maintained v10.4.2. Option 3 (outbox) is the cleanest long-term answer and worth revisiting if Go ever produces to more than one consumer technology, but it's over-engineered now and adds a relay needing its own tests/monitoring. Within option 1, Go replicating the INSERT is bounded/testable: 4 fixed queue names, schema version pinned by exact `pg-boss@10.4.2`, plus a CI contract test against drift.

## Data migration of the 4135 stuck jobs

**Recommend discard, not migrate.** These rows were never validated against a real consumer; payloads may reference IDs whose owning rows changed shape across migrations 002–005. Re-processing 3195 stale `scan-company` + 774 stale `ingest-cv` blindly risks re-triggering work against moved-on data and inconsistent usage-limit counting. Concretely: confirm counts, `ALTER TABLE pgboss.job RENAME TO pgboss_job_orphaned_pre_v10` for forensics, drop before admin install. Optional: a deliberate, per-job-type, reviewed re-enqueue script for idempotent types (scan-company). **This is a product decision — confirm with stakeholder before apply deletes/renames.**

## Testing impact (strict_tdd active; `make test-all`)

- Rewrite `EnsurePgbossStandin` (`rlsdb/harness.go`) + the cv-test wrapper to the real v10 partitioned schema + a registered queue per job name. (Go has no pg-boss lib → raw DDL fixture is the only option; this is a second place tracking pg-boss's schema — flag in design. Mitigate via `PgBoss.getConstructionPlans('pgboss')` dumped by a small Node script.)
- New: Go unit test asserting `Enqueue` errors on zero `RETURNING` rows.
- New: one cross-service integration test (provision + `createQueue` all 4 → Go `Enqueue` → worker `boss` with `migrate:false` dequeues). The single test that would have caught the incident; primary acceptance criterion.

## Risks
- v10 "queue must be registered before send/work" → silent null/zero-rows. Defend in Go (`RETURNING id` count) + ops runbook.
- `ALTER DEFAULT PRIVILEGES` only covers admin-created objects — never delegate queue creation to `app_user`.
- Go hand-replicating v10 `insertJob` → pin exact `pg-boss` version (not `^10.0.0`) + CI contract test.
- 4135 stuck rows = real user-facing impact (scans/CVs that silently never ran) — discard is a product decision, confirm explicitly.
- Test-fixture duplication of the real schema is ongoing maintenance cost.

## Ready for Proposal
Yes. Chosen approach (v10 + registered queues + admin-provisioned + 2 mitigations), data-migration decision (discard w/ confirmation, optional selective re-enqueue), and concrete test additions are all actionable. Surface the stuck-jobs discard decision to the user explicitly — it's product-impacting.
