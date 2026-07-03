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
  // pg-boss v10 invokes the callback with an ARRAY of jobs (batch), while
  // every handler in jobs/ expects a single job and destructures job.data.
  // Unwrap here so all job types are fixed in one place (issue #42).
  await boss.work(jobType, opts, async (jobs) => {
    for (const job of Array.isArray(jobs) ? jobs : [jobs]) {
      await handler(job)
    }
  })
}

export default boss
