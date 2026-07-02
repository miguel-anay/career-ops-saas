// LLM fallback for emails a deterministic parser matched by sender but could
// not extract job cards from. Flag-gated via EMAIL_PARSER_LLM_FALLBACK=true
// (see jobs/ingest-email.mjs) — this module is only ever dynamically
// imported when the flag is set, so the default ingest path stays at 0 LLM
// tokens (Cost Invariant requirement).
//
// Prefixed with _ so it is never mistaken for a registered sender parser
// (mirrors email-parsers/_shared.mjs / providers/_http.mjs convention).
import { evaluate } from '../lib/anthropic.mjs'
import { MAX_HTML_LENGTH } from './_shared.mjs'

const SYSTEM_PROMPT = `You extract job postings from a job-alert email sent by a job board (LinkedIn, Indeed, Computrabajo, Bumeran, etc).
Given the email subject and body, return a JSON array of the job postings found, each shaped as:
{"title": string, "company": string, "url": string}
If no job postings can be found, return an empty array [].
Respond with ONLY the JSON array — no prose, no markdown fences.`

/**
 * Ask Claude to extract job postings from an email a deterministic parser
 * matched by sender but returned zero results for (template drift, etc).
 *
 * @param {{subject: string, html: string, text: string}} decoded
 * @returns {Promise<Array<{title: string, company: string, url: string}>>}
 */
export async function parseEmailWithLLM({ subject, html, text }) {
  // Same cap as the deterministic parsers (_shared.mjs): email body is
  // attacker-influenced content. Truncation (not rejection) is fine here —
  // this is a best-effort fallback, and a partial body still bounds the
  // token cost instead of paying for a multi-MB adversarial message.
  const body = (text || html || '').slice(0, MAX_HTML_LENGTH)
  const userContent = `Subject: ${subject}\n\n${body}`

  const message = await evaluate(
    [{ type: 'text', text: SYSTEM_PROMPT, cache_control: { type: 'ephemeral' } }],
    userContent
  )

  const raw = message?.content?.[0]?.text ?? '[]'
  try {
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}
