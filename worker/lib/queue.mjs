import PgBoss from 'pg-boss'
import 'dotenv/config'

const boss = new PgBoss(process.env.DATABASE_URL)

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
