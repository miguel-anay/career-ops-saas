// One-time pg-boss schema provisioning.
//
// Run by a PRIVILEGED role that OWNS the pgboss schema (e.g. the DB admin),
// NOT the restricted runtime role. app_user has no CREATE on the database by
// design (it is an RLS-subject role), so it cannot let pg-boss self-migrate.
// Instead: install the schema once here as admin, register the 4 known
// queues (also admin-side — app_user has no CREATE, and the
// ALTER DEFAULT PRIVILEGES grant in db/pgboss_grants.sql only covers
// partition tables created by the admin role, see explore item 7), grant
// app_user DML (db/pgboss_grants.sql), and run the worker with
// `migrate: false`.
//
// Order of operations:
//   1. Rename the pre-v10 hand-rolled fake `pgboss.job` table out of the way
//      (forensics only — no row migration, see proposal "Data Migration").
//      Guarded so re-runs are a no-op once already renamed.
//   2. Install the real pg-boss v10 schema (migrate: true).
//   3. Register all 4 queues used by the worker (idempotent — createQueue
//      no-ops if the queue already exists).
//   4. Stop.
//
// Usage (admin connection string):
//   PGBOSS_ADMIN_URL='postgres://careerops:careerops@localhost:5432/careerops' \
//     node worker/scripts/install-pgboss.mjs
import pg from 'pg'
import PgBoss from 'pg-boss'

const url = process.env.PGBOSS_ADMIN_URL || process.env.DATABASE_URL
if (!url) {
  console.error('Set PGBOSS_ADMIN_URL (or DATABASE_URL) to an admin/owner connection string.')
  process.exit(1)
}

// The 4 queue names enqueued by the Go API and consumed by worker/index.mjs.
// Keep in sync with worker/index.mjs's registerWorker() calls.
export const QUEUE_NAMES = ['scan-company', 'evaluate-job', 'generate-pdf', 'ingest-cv']

async function renameOrphanedFakeTable(connectionString) {
  const client = new pg.Client({ connectionString })
  await client.connect()
  try {
    // Guard: only rename if pgboss.job exists AND is not already a pg-boss
    // v10 install (i.e. the pgboss.queue registry table doesn't exist yet).
    // This makes re-runs idempotent: once v10 is installed, pgboss.queue
    // exists and this step becomes a no-op forever.
    const { rows } = await client.query(`
      SELECT
        to_regclass('pgboss.job') IS NOT NULL AS job_exists,
        to_regclass('pgboss.queue') IS NOT NULL AS queue_registry_exists
    `)
    const { job_exists: jobExists, queue_registry_exists: queueRegistryExists } = rows[0]

    if (jobExists && !queueRegistryExists) {
      await client.query('ALTER TABLE pgboss.job RENAME TO pgboss_job_orphaned_pre_v10')
      console.log('Renamed pre-v10 fake pgboss.job -> pgboss.pgboss_job_orphaned_pre_v10 (forensics only).')
    } else if (jobExists && queueRegistryExists) {
      console.log('pgboss.job is already the real v10 partitioned table — skipping rename.')
    } else {
      console.log('No existing pgboss.job table found — skipping rename.')
    }
  } finally {
    await client.end()
  }
}

async function installV10Schema(connectionString) {
  const boss = new PgBoss({ connectionString, schema: 'pgboss', migrate: true })
  await boss.start() // installs + migrates the pgboss schema as the admin role

  for (const name of QUEUE_NAMES) {
    await boss.createQueue(name) // idempotent — no-ops if already registered
    console.log(`Registered queue: ${name}`)
  }

  await boss.stop({ graceful: false })
}

try {
  await renameOrphanedFakeTable(url)
  await installV10Schema(url)
  console.log('pg-boss v10 schema installed and all queues registered on schema "pgboss".')
  process.exit(0)
} catch (err) {
  console.error('pg-boss install failed:', err.message)
  process.exit(1)
}
