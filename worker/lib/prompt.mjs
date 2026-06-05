/**
 * Build the Anthropic messages array for job evaluation.
 *
 * Architecture:
 *   system[0] = static system prompt     → cache_control: ephemeral
 *   system[1] = user CV + profile_json   → cache_control: ephemeral
 *   messages[0] = user content (JD)
 *
 * The two-block caching structure allows prompt caching on the stable
 * [system + CV] prefix while keeping the JD variable (NFR-03).
 *
 * @param {string} userId - UUID of the user (for RLS tenant query)
 * @param {string} jobId - UUID of the job to evaluate
 * @param {object} db - DB interface with tenantQuery method
 * @returns {Promise<{system: Array, messages: Array}>}
 */
export async function buildEvaluationPrompt(userId, jobId, db) {
  const { tenantQuery } = db

  // Fetch user data (CV + profile)
  const userResult = await tenantQuery(
    userId,
    `SELECT cv_markdown, profile_json FROM users WHERE id = $1::uuid LIMIT 1`,
    [userId]
  )
  const user = userResult.rows[0] || {}

  // Fetch job data
  const jobResult = await tenantQuery(
    userId,
    `SELECT scraped_content, title, company, url FROM jobs WHERE id = $1::uuid LIMIT 1`,
    [jobId]
  )
  const job = jobResult.rows[0] || {}

  const cvMarkdown = user.cv_markdown || ''
  const profileJson = typeof user.profile_json === 'string'
    ? user.profile_json
    : JSON.stringify(user.profile_json || {})
  const scrapedContent = job.scraped_content || ''
  const jobTitle = job.title || ''
  const jobCompany = job.company || ''
  const jobUrl = job.url || ''

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

Be honest, specific, and actionable. Reference actual details from the JD and CV.`

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
- **URL**: ${jobUrl}

### Job Description / Scraped Content
${scrapedContent || '(No scraped content available — evaluate from title and company only)'}

---

Please provide a structured evaluation following the 7-block format (A through G) as instructed.`

  return {
    system: [
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
    ],
    messages: [
      {
        role: 'user',
        content: outputContract,
      },
    ],
  }
}
