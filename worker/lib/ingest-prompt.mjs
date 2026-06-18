/**
 * System prompt for the CV ingestion contract (design.md §7).
 *
 * Instructs Claude to act as a CV parser and emit EXACTLY two delimited
 * sections in a fixed order so the regex split in `parseIngestResponse`
 * (worker/jobs/ingest-cv.mjs) is deterministic.
 */
export const INGEST_SYSTEM_PROMPT = `You are a CV ingestion assistant. Given a candidate's raw CV text, produce TWO sections
in this EXACT format and nothing else:

===CV_MARKDOWN===
<the CV reformatted as clean, well-structured markdown — headings, bullet lists,
preserve all factual content, do not invent>

===PROFILE_JSON===
\`\`\`json
{ "candidate": { "full_name": "...", "email": "...", "phone": "...", "location": "...",
    "linkedin": "...", "github": "...", "portfolio_url": "..." },
  "target_roles": { "primary": ["..."], "archetypes": [{ "name": "...", "level": "...", "fit": "..." }] },
  "salary_target": { "min": 0, "max": 0, "currency": "..." },
  "narrative": "..." }
\`\`\`

Rules:
- Output the markers VERBATIM, in order: ===CV_MARKDOWN=== then ===PROFILE_JSON===.
- profile_json MUST be valid JSON inside a \`\`\`json fence.
- Use null/empty for unknown fields; never fabricate contact info or salary.
- candidate.full_name is REQUIRED if present anywhere in the CV.`

/**
 * Build the Claude prompt for a CV ingestion job.
 *
 * Synchronous — the raw CV text is already in the job payload, no DB read
 * required. Returns one cached system block (the extraction contract) and
 * one user message containing the raw CV.
 *
 * @param {string} rawCV - The candidate's raw CV text
 * @returns {{ system: Array<{type: string, text: string, cache_control: object}>, messages: Array<{role: string, content: string}> }}
 */
export function buildIngestPrompt(rawCV) {
  return {
    system: [
      {
        type: 'text',
        text: INGEST_SYSTEM_PROMPT,
        cache_control: { type: 'ephemeral' },
      },
    ],
    messages: [
      {
        role: 'user',
        content: `Here is my raw CV:\n\n${rawCV}`,
      },
    ],
  }
}
