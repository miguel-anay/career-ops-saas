import { tenantQuery, pool } from '../lib/db.mjs'
import { buildIngestPrompt } from '../lib/ingest-prompt.mjs'
import { ingestCV as anthropicIngestCV } from '../lib/anthropic.mjs'
import { ingestCV as openaiCompatIngestCV } from '../lib/openai-compat.mjs'
import { notify } from '../lib/progress.mjs'

/**
 * Parse the two-section ingestion response from Anthropic.
 *
 * Contract (design.md §7): the response MUST contain, in order, a
 * `===CV_MARKDOWN===` section followed by a `===PROFILE_JSON===` section
 * with a fenced ```json block. This guard NEVER throws — on any miss
 * (missing markers, malformed JSON, empty input) it returns the raw text
 * as `cvMarkdown` and `{ parse_error: true, raw }` as `profileJson` so the
 * row is never lost (mirrors `parseEvaluationResponse`, T-58).
 *
 * @param {string} responseText - Raw text from the Anthropic response
 * @returns {{ cvMarkdown: string, profileJson: object }}
 */
export function parseIngestResponse(responseText) {
  const raw = responseText || ''

  if (!raw.trim()) {
    return { cvMarkdown: raw, profileJson: { parse_error: true, raw } }
  }

  try {
    const mdMatch = raw.match(/===CV_MARKDOWN===\s*([\s\S]*?)\s*===PROFILE_JSON===/i)
    const jsonMatch = raw.match(/===PROFILE_JSON===\s*```json\s*([\s\S]*?)```/i)

    if (!mdMatch || !jsonMatch) {
      return { cvMarkdown: raw, profileJson: { parse_error: true, raw } }
    }

    const cvMarkdown = mdMatch[1].trim()
    const profileJson = JSON.parse(jsonMatch[1].trim())

    return { cvMarkdown, profileJson }
  } catch {
    return { cvMarkdown: raw, profileJson: { parse_error: true, raw } }
  }
}

/**
 * Handle an "ingest-cv" pg-boss job.
 *
 * Payload: { user_id, run_id, raw_cv }
 *
 * Flow:
 *   1. Transition the cv_ingestions row to 'processing' before calling Claude
 *   2. Build the ingest prompt and call Claude exactly once
 *   3. Parse the response with the never-throw guard
 *   4. tenantQuery UPDATE users (cv_markdown, profile_json)
 *   5. tenantQuery UPDATE cv_ingestions (status, finished_at)
 *   6. notify(client, run_id, 'ingest.completed' | 'ingest.failed', {...})
 *
 * NOTE: usage.ingestions_count is NOT incremented here. It is metered at
 * enqueue time in the Go API (cv.Service.EnqueueIngest, Seam B) — bumping
 * it again in the worker would double-count the same ingestion.
 *
 * On a parse miss, the row still ends in 'completed' (raw markdown
 * persisted, profile_json = {parse_error:true}); only a thrown Anthropic
 * call (network/API error) drives the row to 'failed'. Either way the row
 * never stays stuck in pending/processing.
 *
 * @param {object} job - pg-boss job object
 * @param {object} job.data - Job payload
 */
export async function handleIngestCV(job) {
  const { user_id, run_id, raw_cv } = job.data
  const client = await pool.connect()

  try {
    await tenantQuery(
      user_id,
      `UPDATE cv_ingestions SET status = 'processing' WHERE id = $1::uuid`,
      [run_id]
    )

    const useAnthropic = (process.env.EVALUATOR || 'anthropic') === 'anthropic'
    const ingestCV = useAnthropic ? anthropicIngestCV : openaiCompatIngestCV

    try {
      const prompt = buildIngestPrompt(raw_cv)
      const response = await ingestCV(prompt.system, prompt.messages[0].content)
      const responseText = response.content?.[0]?.text || ''

      const { cvMarkdown, profileJson } = parseIngestResponse(responseText)

      await tenantQuery(
        user_id,
        `UPDATE users SET cv_markdown = $1, profile_json = $2::jsonb WHERE id = $3::uuid`,
        [cvMarkdown, JSON.stringify(profileJson), user_id]
      )

      await tenantQuery(
        user_id,
        `UPDATE cv_ingestions SET status = 'completed', finished_at = NOW() WHERE id = $1::uuid`,
        [run_id]
      )

      await notify(client, run_id, 'ingest.completed', {
        parse_error: !!profileJson.parse_error,
      })
    } catch (err) {
      await tenantQuery(
        user_id,
        `UPDATE cv_ingestions SET status = 'failed', finished_at = NOW() WHERE id = $1::uuid`,
        [run_id]
      )

      await notify(client, run_id, 'ingest.failed', {
        error: err?.message || 'unknown error',
      })
    }
  } finally {
    client.release()
  }
}
