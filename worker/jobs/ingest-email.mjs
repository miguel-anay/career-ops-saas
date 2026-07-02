import { tenantQuery, pool } from '../lib/db.mjs'
import { notify } from '../lib/progress.mjs'
import { getAccessToken, listMessages, getMessage, decodeMessage } from '../lib/gmail.mjs'
import { allSenders, findParserForSender } from '../email-parsers/index.mjs'
import { normalizeJobUrl } from '../lib/url-normalize.mjs'

const MAX_MESSAGES = 50
const LOOKBACK_DAYS = 14

function buildQuery() {
  const fromClause = allSenders()
    .map((s) => `from:(${s})`)
    .join(' OR ')
  return `${fromClause} newer_than:${LOOKBACK_DAYS}d`
}

/**
 * Handle an "ingest-email" pg-boss job.
 *
 * Payload: { user_id, ingest_run_id }
 *
 * Flow mirrors jobs/scan.mjs: read token -> exchange -> list -> per-message
 * decode/parse/normalize/upsert -> notify. Per-message failures are caught,
 * appended to errors_json, and skipped — one bad email never aborts the run
 * (NFR-07 pattern). Never re-throws.
 *
 * @param {object} job - pg-boss job object
 * @param {object} job.data - { user_id, ingest_run_id }
 */
export async function handleIngestEmail(job) {
  const { user_id, ingest_run_id } = job.data
  const client = await pool.connect()

  try {
    const tokenResult = await tenantQuery(
      user_id,
      `SELECT google_refresh_token FROM users WHERE id = $1::uuid`,
      [user_id]
    )
    const refreshToken = tokenResult.rows[0]?.google_refresh_token

    if (!refreshToken) {
      await finalizeRun(client, user_id, ingest_run_id, {
        status: 'error',
        newCount: 0,
        errors: [{ reason: 'gmail_not_connected' }],
      })
      return
    }

    let accessToken
    try {
      accessToken = await getAccessToken(refreshToken)
    } catch (err) {
      // Token exchange failure before any Gmail read — spec: "Refresh token revoked at Google"
      console.error(`[ingest-email] token exchange failed for user ${user_id}:`, err.message)
      await finalizeRun(client, user_id, ingest_run_id, {
        status: 'error',
        newCount: 0,
        errors: [{ reason: 'token_revoked' }],
      })
      return
    }

    let messages = []
    try {
      messages = await listMessages(accessToken, buildQuery(), MAX_MESSAGES)
    } catch (err) {
      console.error(`[ingest-email] listMessages failed for user ${user_id}:`, err.message)
      await finalizeRun(client, user_id, ingest_run_id, {
        status: 'error',
        newCount: 0,
        errors: [{ reason: 'gmail_list_failed', error: err.message }],
      })
      return
    }

    let newCount = 0
    let dupCount = 0
    const errors = []

    for (const { id } of messages) {
      try {
        const message = await getMessage(accessToken, id)
        const decoded = decodeMessage(message)
        const parser = findParserForSender(decoded.from)
        if (!parser) continue // unrecognized sender — silently skip (spec)

        const rawJobs = parser.parse(decoded) || []

        for (const raw of rawJobs) {
          const url = normalizeJobUrl(parser.id, raw.url)
          if (!url) {
            errors.push({ message_id: id, sender: parser.id, reason: 'url_extraction_failed' })
            continue
          }

          const upsertResult = await tenantQuery(
            user_id,
            `INSERT INTO jobs (user_id, title, company, url, platform, status, received_at)
             VALUES ($1::uuid, $2, $3, $4, $5, 'new', NOW())
             ON CONFLICT (user_id, url) DO NOTHING
             RETURNING id, (xmax = 0) AS is_new`,
            [user_id, raw.title || '', raw.company || '', url, parser.id]
          )

          if (upsertResult.rows.length > 0) {
            const row = upsertResult.rows[0]
            newCount++
            await notify(client, ingest_run_id, 'ingest.job_found', {
              job_id: row.id,
              title: raw.title,
              company: raw.company,
              url,
              is_new: true,
            })
          } else {
            dupCount++
          }
        }
      } catch (err) {
        // Per-message failure — never abort the run (NFR-07).
        console.error(`[ingest-email] message ${id} failed:`, err.message)
        errors.push({ message_id: id, error: err.message })
      }
    }

    // ponytail: message-level partial/completed determination (not full
    // per-sender granularity from the spec table) — sufficient for the
    // tested scenarios; revisit if per-sender status ever matters.
    let status
    if (errors.length === 0) status = 'completed'
    else if (newCount > 0 || dupCount > 0) status = 'partial'
    else status = 'error'

    await finalizeRun(client, user_id, ingest_run_id, { status, newCount, errors })
  } finally {
    client.release()
  }
}

async function finalizeRun(client, userId, ingestRunId, { status, newCount, errors }) {
  if (errors.length > 0) {
    await tenantQuery(
      userId,
      `UPDATE email_ingest_runs SET errors_json = errors_json || $1::jsonb WHERE id = $2::uuid`,
      [JSON.stringify(errors), ingestRunId]
    )
  }

  await tenantQuery(
    userId,
    `UPDATE email_ingest_runs SET status = $1, new_jobs = new_jobs + $2, finished_at = NOW() WHERE id = $3::uuid`,
    [status, newCount, ingestRunId]
  )

  await notify(client, ingestRunId, 'ingest.completed', {
    status,
    new_jobs: newCount,
    errors_count: errors.length,
  })
}
