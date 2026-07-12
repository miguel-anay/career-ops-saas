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
 * System prompt for the CV merge-on-ingest variant (design.md D1).
 *
 * Used when the user already has a non-empty `cv_markdown`. Instructs
 * Claude to produce a COMPREHENSIVE SUPERSET of the existing CV plus the
 * newly pasted text — never a subset — while reconciling same-employer
 * promotions/updates into a single entry instead of duplicating. Emits the
 * SAME two delimited sections as `INGEST_SYSTEM_PROMPT`, so
 * `parseIngestResponse` (the regex split) does not change.
 */
export const INGEST_MERGE_SYSTEM_PROMPT = `You are a CV ingestion assistant merging NEW resume text into an EXISTING
career profile. You are given the candidate's existing CV (already structured
markdown) and newly pasted raw text. Produce a COMPREHENSIVE SUPERSET that
keeps every fact from the existing CV and folds in everything new.

Merge rules:
- NEVER drop a role, employer, project, skill, or achievement that is in the
  existing CV but absent from the new text. The new text is often a tailored
  subset — treat missing items as omitted-for-brevity, not deleted.
- If the new text describes the SAME entry as an existing one (same employer,
  overlapping dates, or an evident promotion/title change at the same company),
  UPDATE that single entry in place — merge the details, keep the most recent
  title and the widest date range. Do NOT emit two entries for one real job.
- If the new text adds a genuinely new role, employer, or skill, ADD it.
- Prefer the more specific / more recent value on any direct conflict (title,
  dates, metrics); never invent facts to reconcile a conflict.
- Keep the result in the same clean markdown structure.

Then produce the two delimited sections in this EXACT format and nothing else:

===CV_MARKDOWN===
<the merged, comprehensive CV as clean markdown>

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
- The merged CV must be a superset: it may never be shorter in factual coverage
  than the existing CV.`

/**
 * Build the Claude prompt for a CV ingestion job.
 *
 * Synchronous — the raw CV text is already in the job payload, no DB read
 * required. When `existingCvMarkdown` is empty (first ingest), uses the
 * original single-section prompt with a raw-only user message. When it is
 * non-empty, uses the merge variant (design.md D1) and labels both texts
 * distinctly in the user message so Claude can reconcile them.
 *
 * @param {string} rawCV - The candidate's raw CV text
 * @param {string} [existingCvMarkdown] - The user's current cv_markdown, if any
 * @returns {{ system: Array<{type: string, text: string, cache_control: object}>, messages: Array<{role: string, content: string}> }}
 */
export function buildIngestPrompt(rawCV, existingCvMarkdown) {
  const systemPromptText = existingCvMarkdown ? INGEST_MERGE_SYSTEM_PROMPT : INGEST_SYSTEM_PROMPT
  const userContent = existingCvMarkdown
    ? `Here is my EXISTING CV (already structured):\n\n${existingCvMarkdown}\n\n---\n\nHere is NEWLY pasted CV text to merge in:\n\n${rawCV}`
    : `Here is my raw CV:\n\n${rawCV}`

  return {
    system: [
      {
        type: 'text',
        text: systemPromptText,
        cache_control: { type: 'ephemeral' },
      },
    ],
    messages: [
      {
        role: 'user',
        content: userContent,
      },
    ],
  }
}
