// Dumps pg-boss's own schema construction DDL via the public static
// PgBoss.getConstructionPlans('pgboss') API.
//
// Why this exists: Go has no pg-boss client library, so the Go test
// fixtures (api/internal/testsupport/rlsdb) cannot call pg-boss directly to
// provision a real v10 schema. Hand-transcribing the DDL into a second Go
// file would create a second place that silently drifts from pg-boss's
// actual schema on every version bump — exactly the kind of duplication
// that caused the original incident this change fixes.
//
// Instead, this script is the SINGLE point that talks to the pg-boss
// library to get the DDL, and writes it to a committed SQL file
// (db/pgboss_schema.generated.sql) that the Go fixture executes verbatim.
// Re-run this script after any pg-boss version bump:
//
//   node worker/scripts/dump-pgboss-schema.mjs
//
// and commit the regenerated db/pgboss_schema.generated.sql alongside the
// package.json version bump, so drift is caught in code review instead of
// at runtime.
import { writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import path from 'node:path'
import PgBoss from 'pg-boss'

const ddl = PgBoss.getConstructionPlans('pgboss')

const header = `-- GENERATED FILE — DO NOT EDIT BY HAND.
--
-- Source of truth: pg-boss's own PgBoss.getConstructionPlans('pgboss')
-- static method (worker/node_modules/pg-boss/src/index.js), dumped by
-- worker/scripts/dump-pgboss-schema.mjs against pg-boss@10.4.2 (the exact
-- version pinned in worker/package.json).
--
-- Consumed by api/internal/testsupport/rlsdb (Go test fixtures) to
-- provision the REAL pg-boss v10 partitioned schema in tests, instead of a
-- hand-rolled fake table. This is also the schema installed in production
-- by worker/scripts/install-pgboss.mjs.
--
-- Re-generate after any pg-boss version bump:
--   node worker/scripts/dump-pgboss-schema.mjs
`

const outPath = path.join(
  path.dirname(fileURLToPath(import.meta.url)),
  '..',
  '..',
  'db',
  'pgboss_schema.generated.sql',
)

writeFileSync(outPath, header + ddl)
console.log(`Wrote ${outPath}`)
