import { tenantQuery, pool } from '../lib/db.mjs'
import { notify } from '../lib/progress.mjs'
import { makeHttpCtx } from '../providers/_http.mjs'

// Provider registry — keyed by provider_id stored in watched_companies
const PROVIDER_REGISTRY = {}

async function loadProviders() {
  if (Object.keys(PROVIDER_REGISTRY).length > 0) return

  const providerModules = [
    '../providers/greenhouse.mjs',
    '../providers/ashby.mjs',
    '../providers/lever.mjs',
    '../providers/recruitee.mjs',
    '../providers/smartrecruiters.mjs',
    '../providers/workable.mjs',
  ]

  for (const modulePath of providerModules) {
    const mod = await import(modulePath)
    const provider = mod.default
    if (provider?.id) {
      PROVIDER_REGISTRY[provider.id] = provider
    }
  }
}

/**
 * Handle a "scan-company" pg-boss job.
 *
 * Payload: { user_id, company_id, scan_run_id }
 *
 * Flow:
 *   1. Fetch watched_company record from DB
 *   2. Load provider by provider_id
 *   3. Call provider.fetch() with HTTP context
 *   4. UPSERT each job (INSERT ... ON CONFLICT DO NOTHING → detect is_new)
 *   5. NOTIFY scan.job_found for new jobs
 *   6. NOTIFY scan.company.done or scan.company.error
 *   7. Errors from providers are caught and reported — NOT re-thrown (NFR-07)
 *
 * @param {object} job - pg-boss job object
 * @param {object} job.data - Job payload
 */
export async function handleScanCompany(job) {
  await loadProviders()

  const { user_id, company_id, scan_run_id } = job.data
  const client = await pool.connect()

  try {
    // Fetch company details. For catalog-linked companies (company_id set) the
    // careers_url / provider / ATS API URL are read from companies_catalog via
    // COALESCE, so they stay fresh if the catalog entry is updated (no drift
    // against the inline snapshot copied at watch time). Manual companies
    // (company_id NULL) fall back to their own inline columns.
    const companyResult = await tenantQuery(
      user_id,
      `SELECT wc.id, wc.name,
              COALESCE(cc.careers_url, wc.careers_url) AS careers_url,
              COALESCE(cc.provider_id, wc.provider_id) AS provider_id,
              COALESCE(cc.ats_api_url, wc.ats_api_url) AS ats_api_url
       FROM watched_companies wc
       LEFT JOIN companies_catalog cc ON cc.id = wc.company_id
       WHERE wc.id = $1::uuid AND wc.user_id = $2::uuid AND wc.enabled = true
       LIMIT 1`,
      [company_id, user_id]
    )

    const company = companyResult.rows[0]
    if (!company) {
      console.warn(`[scan] Company ${company_id} not found or disabled for user ${user_id}`)
      return
    }

    const providerId = company.provider_id
    const provider = PROVIDER_REGISTRY[providerId]

    if (!provider) {
      await notify(client, scan_run_id, 'scan.company.error', {
        company_id: company.id,
        company: company.name,
        provider: providerId,
        error: `Unknown provider: ${providerId}`,
      })
      return
    }

    // Build HTTP context for providers
    const ctx = makeHttpCtx()

    // Build provider entry format
    const entry = {
      name: company.name,
      careers_url: company.careers_url,
      ats_api_url: company.ats_api_url,
    }

    let foundJobs = []
    let providerError = null

    // Call provider — catch errors for graceful degrade (NFR-07)
    try {
      foundJobs = await provider.fetch(entry, ctx)
    } catch (err) {
      providerError = err
      console.error(`[scan] Provider ${providerId} failed for ${company.name}:`, err.message)

      await notify(client, scan_run_id, 'scan.company.error', {
        company_id: company.id,
        company: company.name,
        provider: providerId,
        error: err.message,
      })

      // Record error in scan_runs.errors_json
      await tenantQuery(
        user_id,
        `UPDATE scan_runs
         SET errors_json = errors_json || $1::jsonb
         WHERE id = $2::uuid`,
        [
          JSON.stringify([{ company_id: company.id, company: company.name, provider: providerId, error: err.message }]),
          scan_run_id,
        ]
      )

      return  // Graceful exit — do NOT re-throw (NFR-07)
    }

    // UPSERT jobs and track new ones
    let newCount = 0
    const upsertedJobs = []

    for (const j of foundJobs) {
      if (!j.url) continue

      try {
        const upsertResult = await tenantQuery(
          user_id,
          `INSERT INTO jobs (user_id, title, company, url, platform, status, received_at)
           VALUES ($1::uuid, $2, $3, $4, $5, 'new', NOW())
           ON CONFLICT (user_id, url) DO NOTHING
           RETURNING id, (xmax = 0) AS is_new`,
          [user_id, j.title || '', j.company || company.name, j.url, providerId]
        )

        if (upsertResult.rows.length > 0) {
          const row = upsertResult.rows[0]
          upsertedJobs.push({ ...row, title: j.title, url: j.url })

          if (row.is_new) {
            newCount++
            await notify(client, scan_run_id, 'scan.job_found', {
              job_id: row.id,
              title: j.title,
              company: j.company || company.name,
              url: j.url,
              is_new: true,
            })
          }
        }
      } catch (err) {
        // Log upsert errors but continue processing other jobs
        console.error(`[scan] Failed to upsert job ${j.url}:`, err.message)
      }
    }

    // Update scan_runs new_jobs count
    await tenantQuery(
      user_id,
      `UPDATE scan_runs
       SET new_jobs = new_jobs + $1
       WHERE id = $2::uuid`,
      [newCount, scan_run_id]
    )

    // NOTIFY scan.company.done
    await notify(client, scan_run_id, 'scan.company.done', {
      company_id: company.id,
      company: company.name,
      found: foundJobs.length,
      new: newCount,
    })
  } finally {
    client.release()
  }
}

/**
 * Finalize a scan run after all companies have been processed.
 *
 * Updates scan_runs.status to 'completed' or 'partial' (partial if any errors),
 * sets finished_at, and emits scan.completed NOTIFY.
 *
 * @param {string} userId
 * @param {string} scanRunId
 * @param {{ newJobs: number, errors: Array }} summary
 * @param {import('pg').PoolClient} pgClient
 */
export async function finalizeScanRun(userId, scanRunId, summary, pgClient) {
  const { newJobs, errors, companiesOk } = summary
  const hasErrors = errors && errors.length > 0
  const status = hasErrors ? 'partial' : 'completed'
  const companiesFailed = errors ? errors.length : 0

  await tenantQuery(
    userId,
    `UPDATE scan_runs
     SET status = $1, finished_at = NOW(), new_jobs = $2
     WHERE id = $3::uuid`,
    [status, newJobs, scanRunId]
  )

  await notify(pgClient, scanRunId, 'scan.completed', {
    status,
    new_jobs: newJobs,
    companies_ok: typeof companiesOk === 'number' ? companiesOk : 0,
    companies_failed: companiesFailed,
  })
}
