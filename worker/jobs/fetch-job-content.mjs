import { tenantQuery } from '../lib/db.mjs'
import { fetchPageText } from '../shared/fetch-page.mjs'
import { isHostAllowed } from '../lib/url-normalize.mjs'

/**
 * Handle a "fetch-job-content" pg-boss job.
 *
 * Payload: { user_id, job_id }
 *
 * Flow:
 *   1. Read the job URL from the DB (within tenant RLS)
 *   2. SSRF gate: verify host is allowlisted
 *   3. Navigate with Playwright and extract text
 *   4. Write scraped_content back to the job row
 *
 * @param {object} job - pg-boss job object
 * @param {object} job.data - { user_id, job_id }
 */
export async function handleFetchJobContent(job) {
  const { user_id, job_id } = job.data

  // 1. Read the job URL (tenant-scoped)
  const jobResult = await tenantQuery(
    user_id,
    `SELECT id, url FROM jobs WHERE id = $1::uuid LIMIT 1`,
    [job_id]
  )
  const jobRow = jobResult.rows[0]
  if (!jobRow) {
    console.error(`[fetch-job-content] job ${job_id} not found for user ${user_id}`)
    return
  }

  const url = jobRow.url
  let parsed
  try {
    parsed = new URL(url)
  } catch {
    console.error(`[fetch-job-content] invalid URL for job ${job_id}: ${url}`)
    return
  }

  // 2. SSRF gate — double-check host is allowlisted
  if (!isHostAllowed(parsed.hostname)) {
    console.error(`[fetch-job-content] host not allowed for job ${job_id}: ${parsed.hostname}`)
    return
  }

  // 3. Fetch with Playwright
  let pageText
  try {
    pageText = await fetchPageText(url)
  } catch (err) {
    console.error(`[fetch-job-content] Playwright fetch failed for job ${job_id}:`, err.message)
    return
  }

  if (!pageText || !pageText.trim()) {
    console.error(`[fetch-job-content] empty content for job ${job_id}`)
    return
  }

  // 4. Write scraped_content (tenant-scoped)
  await tenantQuery(
    user_id,
    `UPDATE jobs SET scraped_content = $1 WHERE id = $2::uuid`,
    [pageText.trim(), job_id]
  )

  console.log(`[fetch-job-content] success: job ${job_id}, ${pageText.trim().length} chars`)
}
