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
 *   2. Pre-read the existing cv_markdown/profile_json (merge-on-ingest, D1)
 *   3. Build the ingest prompt (merge variant if an existing CV exists) and
 *      call Claude exactly once
 *   4. Parse the response with the never-throw guard
 *   5. Sanity guard (D2): skip the destructive UPDATE if Claude returned a
 *      parse error over an already-good profile/CV
 *   6. tenantQuery UPDATE users (cv_markdown, profile_json)
 *   7. tenantQuery UPDATE cv_ingestions (status, finished_at)
 *   8. notify(client, run_id, 'ingest.completed' | 'ingest.failed', {...})
 *
 * NOTE: usage.ingestions_count is NOT incremented here. It is metered at
 * enqueue time in the Go API (cv.Service.EnqueueIngest, Seam B) — bumping
 * it again in the worker would double-count the same ingestion.
 *
 * On a parse miss over an empty/absent prior profile, the row still ends in
 * 'completed' (raw markdown persisted, profile_json = {parse_error:true}) —
 * nothing valuable to protect. On a parse miss over a GOOD prior profile,
 * the sanity guard skips the write entirely and the row ends in 'failed'
 * instead, preserving the existing cv_markdown/profile_json untouched. A
 * thrown Anthropic call (network/API error) also drives the row to
 * 'failed'. Either way the row never stays stuck in pending/processing.
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

    const existingResult = await tenantQuery(
      user_id,
      `SELECT cv_markdown, profile_json FROM users WHERE id = $1::uuid`,
      [user_id]
    )
    const existingCv = existingResult.rows[0]?.cv_markdown || ''
    const existingProfile = existingResult.rows[0]?.profile_json || {}

    const useAnthropic = (process.env.EVALUATOR || 'anthropic') === 'anthropic'
    const ingestCV = useAnthropic ? anthropicIngestCV : openaiCompatIngestCV

    try {
      const prompt = buildIngestPrompt(raw_cv, existingCv)
      const response = await ingestCV(prompt.system, prompt.messages[0].content)
      const responseText = response.content?.[0]?.text || ''

      const { cvMarkdown, profileJson } = parseIngestResponse(responseText)

      // Sanity guard (D2): a parse-error response must never overwrite a
      // good existing profile/CV. If there is nothing valuable to protect
      // (first ingest, or an empty prior profile), fall through as before.
      const parseErrored = profileJson.parse_error === true
      const hadGoodProfile =
        existingProfile && !existingProfile.parse_error && Object.keys(existingProfile).length > 0
      const hadGoodCv = existingCv.trim().length > 0

      if (parseErrored && (hadGoodProfile || hadGoodCv)) {
        await tenantQuery(
          user_id,
          `UPDATE cv_ingestions SET status = 'failed', finished_at = NOW() WHERE id = $1::uuid`,
          [run_id]
        )
        await notify(client, run_id, 'ingest.failed', {
          error: 'parse_error_preserved_existing',
        })
        return
      }

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
