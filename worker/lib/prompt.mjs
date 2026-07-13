/**
 * Build the Anthropic messages array for job evaluation.
 *
 * Architecture:
 *   system[0] = static system prompt     → cache_control: ephemeral
 *   system[1] = user CV + profile_json   → cache_control: ephemeral
 *   system[2] = article digests (optional, omitted if user has none) → cache_control: ephemeral
 *   messages[0] = user content (JD)
 *
 * The two-block caching structure allows prompt caching on the stable
 * [system + CV] prefix while keeping the JD variable (NFR-03). The third
 * block is appended AFTER the first two so it never invalidates their
 * cache breakpoint (article-digest design.md Decision 5).
 *
 * @param {string} userId - UUID of the user (for RLS tenant query)
 * @param {string} jobId - UUID of the job to evaluate
 * @param {object} db - DB interface with tenantQuery method
 * @returns {Promise<{system: Array, messages: Array}>}
 */
/**
 * Human-readable posting age ("posted N days ago") from a job's
 * `received_at` timestamp, or null when unavailable (e.g. legacy rows
 * ingested before this column was backfilled).
 *
 * @param {string | Date | null | undefined} receivedAt
 * @returns {string | null}
 */
function describePostingAge(receivedAt) {
  if (!receivedAt) return null
  const receivedMs = new Date(receivedAt).getTime()
  if (Number.isNaN(receivedMs)) return null
  const days = Math.max(0, Math.floor((Date.now() - receivedMs) / (24 * 60 * 60 * 1000)))
  return `posted ${days} day${days === 1 ? '' : 's'} ago`
}

/**
 * Compute the effective profile: a shallow, top-level-key overlay of
 * `profileOverrides` onto `profileJson` (design.md D3). Deliberately
 * duplicated in the Go `profile` package (D2/job-content-fetch precedent —
 * no cross-language sharing for a 4-line merge).
 *
 * @param {string | object | null | undefined} profileJson
 * @param {string | object | null | undefined} profileOverrides
 * @returns {object}
 */
// ponytail: 4-line shallow merge, duplicated per D2/job-content-fetch precedent
export function mergeProfile(profileJson, profileOverrides) {
  const base = typeof profileJson === 'string' ? JSON.parse(profileJson || '{}') : (profileJson || {})
  const ov = typeof profileOverrides === 'string' ? JSON.parse(profileOverrides || '{}') : (profileOverrides || {})
  return { ...base, ...ov }
}

const DIGEST_PER_ENTRY_MAX = 4000
const DIGEST_TOTAL_MAX = 24000

/**
 * Render the user's article_digests as a bounded, cached third system block,
 * or null when the user has none (article-digest design.md Decision 5).
 *
 * Per-entry cap is applied FIRST (a single giant entry can never monopolize
 * the block), THEN entries accumulate into a running total — once the next
 * whole entry would breach the ceiling, it (and every older entry after it)
 * is dropped entirely. Entries are never spliced mid-way.
 *
 * @param {string} userId
 * @param {Function} tenantQuery
 * @returns {Promise<string | null>}
 */
async function buildDigestBlock(userId, tenantQuery) {
  const digestResult = await tenantQuery(
    userId,
    `SELECT title, content_md FROM article_digests
     WHERE user_id = $1::uuid ORDER BY created_at DESC LIMIT 20`,
    [userId]
  )
  const digests = digestResult?.rows || []
  if (digests.length === 0) return null

  const entries = []
  let runningTotal = 0

  for (const digest of digests) {
    let content = digest.content_md || ''
    if (content.length > DIGEST_PER_ENTRY_MAX) {
      content = `${content.slice(0, DIGEST_PER_ENTRY_MAX)}\n…[truncated]`
    }
    const entry = `### ${digest.title}\n${content}`
    if (runningTotal + entry.length > DIGEST_TOTAL_MAX) break
    entries.push(entry)
    runningTotal += entry.length
  }

  if (entries.length === 0) return null
  return `## Project Proof Points\n\n${entries.join('\n\n')}`
}

export async function buildEvaluationPrompt(userId, jobId, db) {
  const { tenantQuery } = db

  // Fetch user data (CV + profile)
  const userResult = await tenantQuery(
    userId,
    `SELECT cv_markdown, profile_json, profile_overrides FROM users WHERE id = $1::uuid LIMIT 1`,
    [userId]
  )
  const user = userResult.rows[0] || {}

  // Fetch job data
  const jobResult = await tenantQuery(
    userId,
    `SELECT scraped_content, title, company, url, received_at FROM jobs WHERE id = $1::uuid LIMIT 1`,
    [jobId]
  )
  const job = jobResult.rows[0] || {}

  const cvMarkdown = user.cv_markdown || ''
  const profileJson = JSON.stringify(mergeProfile(user.profile_json, user.profile_overrides))
  const scrapedContent = job.scraped_content || ''
  const jobTitle = job.title || ''
  const jobCompany = job.company || ''
  const jobUrl = job.url || ''
  const postingAge = describePostingAge(job.received_at)

  const staticSystemPrompt = `You are an expert career advisor and recruiter with 15+ years of experience evaluating job fits.

Your task is to evaluate a job opportunity against a candidate's CV and career profile.

You must produce a structured evaluation with 7 blocks (A through G):

## Block A — Role & Company Fit
Evaluate how well the role and company align with the candidate's career trajectory, target roles, and values.
Score: X.X/5

## Block B — Technical Match
Evaluate the technical skills overlap — required vs. present, gaps, and strengths.
Score: X.X/5

## Block C — Compensation
Evaluate the compensation (salary, equity, benefits) against the candidate's targets and market rate.
Score: X.X/5

## Block D — Growth & Impact
Evaluate career growth potential, learning opportunities, and impact scope.
Score: X.X/5

## Block E — Culture & Location
Evaluate culture fit, remote/hybrid policy, location requirements, and work-life balance signals.
Score: X.X/5

## Block F — Red Flags
Identify any concerning signals: vague descriptions, unrealistic requirements, toxic culture signals, financial instability.
Score: X.X/5 (5 = no red flags)

## Block G — Posting Legitimacy
Assess if this is a real, active job posting vs. ghost posting, fishing expedition, or outdated listing.
Tier: 1-5 (1 = Verified Direct, 5 = Suspicious/Ghost)

End with: **Overall Score: X.X/5**

Be honest, specific, and actionable. Reference actual details from the JD and CV.

Write the FULL evaluation in Spanish (es-AR). Every block, score description, analysis, and summary must be in Spanish.

Additional guidance:
- Map the candidate's relevant CV experience to STAR-format achievements (Situation, Task, Action, Result) the candidate could use in an interview for this role.
- Include concrete negotiation guidance (e.g. target compensation range, leverage points) informed by the JD and the candidate's profile.
- If a posting age is provided, factor it into Block G's legitimacy assessment (a very old, unrefreshed posting is a legitimacy signal).`

  const cvAndProfileBlock = `## Candidate Profile

### CV / Resume
${cvMarkdown}

### Career Profile (JSON)
\`\`\`json
${profileJson}
\`\`\``

  const outputContract = `Evaluate the following job opportunity against my profile.

### Job Details
- **Title**: ${jobTitle}
- **Company**: ${jobCompany}
- **URL**: ${jobUrl}${postingAge ? `\n- **Posting age**: ${postingAge}` : ''}

### Job Description / Scraped Content
${scrapedContent || '(No scraped content available — evaluate from title and company only)'}

---

Please provide a structured evaluation following the 7-block format (A through G) as instructed.`

  const digestBlockText = await buildDigestBlock(userId, tenantQuery)

  const system = [
    {
      type: 'text',
      text: staticSystemPrompt,
      cache_control: { type: 'ephemeral' },
    },
    {
      type: 'text',
      text: cvAndProfileBlock,
      cache_control: { type: 'ephemeral' },
    },
  ]
  if (digestBlockText) {
    system.push({
      type: 'text',
      text: digestBlockText,
      cache_control: { type: 'ephemeral' },
    })
  }

  return {
    system,
    messages: [
      {
        role: 'user',
        content: outputContract,
      },
    ],
  }
}
