import { tenantQuery } from '../lib/db.mjs'
import { buildEvaluationPrompt } from '../lib/prompt.mjs'
import { evaluate } from '../lib/anthropic.mjs'

/**
 * Parse the 7-block evaluation response from Anthropic.
 *
 * Extracts blocks A-G and overall score. If parsing fails, returns a
 * parse_error sentinel so the row is never lost (T-58).
 *
 * @param {string} responseText - Raw text from Anthropic response
 * @returns {{ blocks: object, score: number | null, contentMd: string }}
 */
function parseEvaluationResponse(responseText) {
  if (!responseText || !responseText.trim()) {
    return {
      blocks: { parse_error: true, raw: responseText },
      score: null,
      contentMd: responseText || '',
    }
  }

  try {
    const blocks = {}

    // Extract blocks A through G
    const blockPattern = /##\s+Block\s+([A-G])\s*[—–-]\s*([^\n]+)([\s\S]*?)(?=##\s+Block\s+[A-G]|##\s+Overall|\*\*Overall Score|$)/gi
    let match
    while ((match = blockPattern.exec(responseText)) !== null) {
      const blockKey = `block${match[1].toUpperCase()}`
      const blockTitle = match[2].trim()
      const blockContent = match[3].trim()

      // Extract score from block content
      const scoreMatch = blockContent.match(/Score:\s*(\d+(?:\.\d+)?)\s*\/\s*5/i)
      const score = scoreMatch ? parseFloat(scoreMatch[1]) : null

      blocks[blockKey] = {
        title: blockTitle,
        content: blockContent,
        score,
      }
    }

    // Extract overall score
    const overallMatch = responseText.match(/\*\*Overall Score:\s*(\d+(?:\.\d+)?)\s*\/\s*5\*\*/i)
    const overallScore = overallMatch ? parseFloat(overallMatch[1]) : null

    // If we couldn't extract meaningful blocks, mark as parse error
    if (Object.keys(blocks).length === 0) {
      return {
        blocks: { parse_error: true, raw: responseText },
        score: null,
        contentMd: responseText,
      }
    }

    return {
      blocks,
      score: overallScore,
      contentMd: responseText,
    }
  } catch (err) {
    // T-58: parse error guard — never lose the row
    return {
      blocks: { parse_error: true, raw: responseText },
      score: null,
      contentMd: responseText,
    }
  }
}

/**
 * Handle an "evaluate-job" pg-boss job.
 *
 * Payload: { user_id, job_id }
 *
 * Flow:
 *   1. Build Anthropic prompt (with 2-block caching)
 *   2. Call Anthropic claude-sonnet-4-6
 *   3. Parse 7-block response (A-G + overall score)
 *   4. INSERT into applications
 *   5. INSERT into reports (with blocks_json)
 *   6. UPSERT usage (evaluations_count +1 for current month)
 *   7. UPDATE jobs.status to 'evaluated'
 *
 * T-58: If parsing throws or produces no blocks, persist { parse_error: true, raw }
 * so the row is never lost.
 *
 * @param {object} job - pg-boss job object
 * @param {object} job.data - Job payload
 */
export async function handleEvaluateJob(job) {
  const { user_id, job_id } = job.data

  // Build prompt using the 2-block caching structure
  const promptData = await buildEvaluationPrompt(user_id, job_id, { tenantQuery })

  // Call Anthropic
  const response = await evaluate(promptData.system, promptData.messages?.[0]?.content || '')

  const responseText = response.content?.[0]?.text || ''

  // Parse response (T-58: parse error guard)
  const { blocks, score, contentMd } = parseEvaluationResponse(responseText)

  const currentMonth = new Date().toISOString().slice(0, 7)  // YYYY-MM

  // INSERT into applications
  const appResult = await tenantQuery(
    user_id,
    `INSERT INTO applications (user_id, job_id, score, status, notes)
     VALUES ($1::uuid, $2::uuid, $3, 'Evaluated', $4)
     RETURNING id`,
    [user_id, job_id, score, blocks.parse_error ? 'Evaluation completed (parse error in blocks)' : null]
  )

  const applicationId = appResult.rows[0]?.id

  // INSERT into reports (always — T-58 ensures blocks_json is set even on parse error)
  await tenantQuery(
    user_id,
    `INSERT INTO reports (user_id, application_id, content_md, blocks_json)
     VALUES ($1::uuid, $2::uuid, $3, $4::jsonb)
     RETURNING id`,
    [user_id, applicationId, contentMd, JSON.stringify(blocks)]
  )

  // UPSERT usage — increment evaluations_count for current month
  await tenantQuery(
    user_id,
    `INSERT INTO usage (user_id, month, evaluations_count, pdfs_count)
     VALUES ($1::uuid, $2, 1, 0)
     ON CONFLICT (user_id, month)
     DO UPDATE SET evaluations_count = usage.evaluations_count + 1`,
    [user_id, currentMonth]
  )

  // UPDATE jobs.status to 'evaluated'
  await tenantQuery(
    user_id,
    `UPDATE jobs SET status = 'evaluated' WHERE id = $1::uuid AND user_id = $2::uuid`,
    [job_id, user_id]
  )
}
