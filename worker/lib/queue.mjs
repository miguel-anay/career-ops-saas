import PgBoss from 'pg-boss'
import 'dotenv/config'

// The worker runs as the restricted RLS role (app_user), which has no CREATE
// on the database. The pgboss schema is provisioned out-of-band by the admin
// (worker/scripts/install-pgboss.mjs + db/pgboss_grants.sql), so we disable
// self-migration here: start() only verifies the schema, it does not run DDL.
const boss = new PgBoss({
  connectionString: process.env.DATABASE_URL,
  schema: 'pgboss',
  migrate: false,
})

let started = false

/**
 * Start the pg-boss instance. Idempotent — safe to call multiple times.
 */
export async function start() {
  if (!started) {
    await boss.start()
    started = true
  }
  return boss
}

/**
 * Register a job worker handler.
 *
 * @param {string} jobType - Job queue name (e.g., 'scan-company')
 * @param {(job: object) => Promise<void>} handler - Async handler function
 * @param {object} [opts] - pg-boss worker options
 */
export async function registerWorker(jobType, handler, opts = {}) {
  await boss.work(jobType, opts, handler)
}

export default boss
