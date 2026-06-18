import { vi, describe, it, expect, beforeEach } from 'vitest'

const mockTenantQuery = vi.fn()
const mockPoolConnect = vi.fn()
const mockBuildIngestPrompt = vi.fn()
const mockIngestCV = vi.fn()
const mockNotify = vi.fn()

vi.mock('../../lib/db.mjs', () => ({
  tenantQuery: mockTenantQuery,
  pool: { connect: mockPoolConnect },
}))

vi.mock('../../lib/ingest-prompt.mjs', () => ({
  buildIngestPrompt: mockBuildIngestPrompt,
}))

vi.mock('../../lib/anthropic.mjs', () => ({
  ingestCV: mockIngestCV,
}))

vi.mock('../../lib/progress.mjs', () => ({
  notify: mockNotify,
}))

const { parseIngestResponse, handleIngestCV } = await import('../../jobs/ingest-cv.mjs')

describe('parseIngestResponse', () => {
  it('parses a valid 2-section response into cvMarkdown + profileJson', () => {
    const responseText = `===CV_MARKDOWN===
# Jane Doe
## Experience
- Senior Engineer at Acme

===PROFILE_JSON===
\`\`\`json
{"candidate":{"full_name":"Jane Doe","email":"","phone":"","location":"","linkedin":"","github":"","portfolio_url":""},"target_roles":{"primary":[],"archetypes":[]},"salary_target":{"min":0,"max":0,"currency":""},"narrative":""}
\`\`\`
`
    const result = parseIngestResponse(responseText)

    expect(result.profileJson.parse_error).toBeUndefined()
    expect(result.profileJson.candidate.full_name).toBe('Jane Doe')
    expect(result.cvMarkdown).toContain('# Jane Doe')
    expect(result.cvMarkdown).toContain('Senior Engineer at Acme')
  })

  it('returns parse_error:true when markers are missing', () => {
    const responseText = 'Just a plain text response with no markers at all.'

    const result = parseIngestResponse(responseText)

    expect(result.profileJson).toEqual({ parse_error: true, raw: responseText })
    expect(result.cvMarkdown).toBe(responseText)
  })

  it('returns parse_error:true when the JSON block is malformed', () => {
    const responseText = `===CV_MARKDOWN===
# Jane Doe

===PROFILE_JSON===
\`\`\`json
{ this is not valid json,,, }
\`\`\`
`
    const result = parseIngestResponse(responseText)

    expect(result.profileJson).toEqual({ parse_error: true, raw: responseText })
    expect(result.cvMarkdown).toBe(responseText)
  })

  it('returns parse_error:true on an empty string and never throws', () => {
    expect(() => parseIngestResponse('')).not.toThrow()
    const result = parseIngestResponse('')
    expect(result.profileJson).toEqual({ parse_error: true, raw: '' })
    expect(result.cvMarkdown).toBe('')
  })

  it('returns parse_error:true for markdown-only content (missing PROFILE_JSON marker)', () => {
    const responseText = `===CV_MARKDOWN===
# Jane Doe
## Experience
- Senior Engineer at Acme
`
    const result = parseIngestResponse(responseText)

    expect(result.profileJson).toEqual({ parse_error: true, raw: responseText })
    expect(result.cvMarkdown).toBe(responseText)
  })

  it('never throws regardless of input shape (null-ish/garbage)', () => {
    expect(() => parseIngestResponse(null)).not.toThrow()
    expect(() => parseIngestResponse(undefined)).not.toThrow()
  })
})

describe('handleIngestCV', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPoolConnect.mockResolvedValue({ release: vi.fn() })
  })

  const validResponseText = `===CV_MARKDOWN===
# Jane Doe

===PROFILE_JSON===
\`\`\`json
{"candidate":{"full_name":"Jane Doe","email":"","phone":"","location":"","linkedin":"","github":"","portfolio_url":""},"target_roles":{"primary":[],"archetypes":[]},"salary_target":{"min":0,"max":0,"currency":""},"narrative":""}
\`\`\`
`

  it('happy path: one Anthropic call, tenantQuery writes users + cv_ingestions completed, notify ingest.completed', async () => {
    mockBuildIngestPrompt.mockReturnValue({
      system: [{ type: 'text', text: 'system prompt', cache_control: { type: 'ephemeral' } }],
      messages: [{ role: 'user', content: 'Here is my raw CV:\n\nraw text' }],
    })
    mockIngestCV.mockResolvedValue({ content: [{ type: 'text', text: validResponseText }] })
    mockTenantQuery.mockResolvedValue({ rows: [] })

    const job = { data: { user_id: 'user-1', run_id: 'run-1', raw_cv: 'raw text' } }

    await handleIngestCV(job)

    // exactly one Anthropic call
    expect(mockIngestCV).toHaveBeenCalledTimes(1)

    // tenantQuery used for: processing transition, users update, cv_ingestions completed update
    expect(mockTenantQuery).toHaveBeenCalled()
    for (const call of mockTenantQuery.mock.calls) {
      expect(call[0]).toBe('user-1')
    }

    const usersUpdateCall = mockTenantQuery.mock.calls.find(
      c => typeof c[1] === 'string' && c[1].toLowerCase().includes('update users')
    )
    expect(usersUpdateCall).toBeDefined()
    expect(usersUpdateCall[1].toLowerCase()).toContain('cv_markdown')
    expect(usersUpdateCall[1].toLowerCase()).toContain('profile_json')

    const processingCall = mockTenantQuery.mock.calls.find(
      c => typeof c[1] === 'string' &&
        c[1].toLowerCase().includes('cv_ingestions') &&
        c[1].toLowerCase().includes('processing')
    )
    expect(processingCall).toBeDefined()

    const completedCall = mockTenantQuery.mock.calls.find(
      c => typeof c[1] === 'string' &&
        c[1].toLowerCase().includes('cv_ingestions') &&
        c[1].toLowerCase().includes('completed')
    )
    expect(completedCall).toBeDefined()
    expect(completedCall[1].toLowerCase()).toContain('finished_at')

    // NO usage write — usage is metered at enqueue time in the Go API (Seam B)
    const usageCall = mockTenantQuery.mock.calls.find(
      c => typeof c[1] === 'string' && c[1].toLowerCase().includes('usage')
    )
    expect(usageCall).toBeUndefined()

    // notify called with positional args (client, run_id, event, data)
    expect(mockNotify).toHaveBeenCalledTimes(1)
    const [, notifiedRunId, notifiedEvent, notifiedData] = mockNotify.mock.calls[0]
    expect(notifiedRunId).toBe('run-1')
    expect(notifiedEvent).toBe('ingest.completed')
    expect(notifiedData.parse_error).toBe(false)
  })

  it('parse-miss path: raw persisted, profile_json {parse_error:true}, status still completed, notify carries parse_error:true', async () => {
    const garbledText = 'no markers here at all'
    mockBuildIngestPrompt.mockReturnValue({
      system: [],
      messages: [{ role: 'user', content: 'raw text' }],
    })
    mockIngestCV.mockResolvedValue({ content: [{ type: 'text', text: garbledText }] })
    mockTenantQuery.mockResolvedValue({ rows: [] })

    const job = { data: { user_id: 'user-2', run_id: 'run-2', raw_cv: 'raw text' } }

    await handleIngestCV(job)

    const usersUpdateCall = mockTenantQuery.mock.calls.find(
      c => typeof c[1] === 'string' && c[1].toLowerCase().includes('update users')
    )
    expect(usersUpdateCall).toBeDefined()
    expect(usersUpdateCall[2]).toContain(garbledText)
    const profileJsonParam = usersUpdateCall[2].find(p => typeof p === 'string' && p.includes('parse_error'))
    expect(profileJsonParam).toBeDefined()
    expect(JSON.parse(profileJsonParam)).toEqual({ parse_error: true, raw: garbledText })

    // status is still 'completed' on a parse miss (per spec Requirement 3 design note: completed, not failed)
    const completedCall = mockTenantQuery.mock.calls.find(
      c => typeof c[1] === 'string' &&
        c[1].toLowerCase().includes('cv_ingestions') &&
        c[1].toLowerCase().includes('completed')
    )
    expect(completedCall).toBeDefined()

    expect(mockNotify).toHaveBeenCalledTimes(1)
    const [, notifiedRunId, notifiedEvent, notifiedData] = mockNotify.mock.calls[0]
    expect(notifiedRunId).toBe('run-2')
    expect(notifiedEvent).toBe('ingest.completed')
    expect(notifiedData.parse_error).toBe(true)
  })

  it('Anthropic-throws path: status set to failed, notify ingest.failed, row never left pending/processing', async () => {
    mockBuildIngestPrompt.mockReturnValue({
      system: [],
      messages: [{ role: 'user', content: 'raw text' }],
    })
    mockIngestCV.mockRejectedValue(new Error('upstream 503'))
    mockTenantQuery.mockResolvedValue({ rows: [] })

    const job = { data: { user_id: 'user-3', run_id: 'run-3', raw_cv: 'raw text' } }

    await expect(handleIngestCV(job)).resolves.not.toThrow()

    const failedCall = mockTenantQuery.mock.calls.find(
      c => typeof c[1] === 'string' &&
        c[1].toLowerCase().includes('cv_ingestions') &&
        c[1].toLowerCase().includes('failed')
    )
    expect(failedCall).toBeDefined()
    expect(failedCall[1].toLowerCase()).toContain('finished_at')

    // no completed status written in this path
    const completedCall = mockTenantQuery.mock.calls.find(
      c => typeof c[1] === 'string' &&
        c[1].toLowerCase().includes('cv_ingestions') &&
        c[1].toLowerCase().includes("'completed'")
    )
    expect(completedCall).toBeUndefined()

    expect(mockNotify).toHaveBeenCalledTimes(1)
    const [, notifiedRunId, notifiedEvent] = mockNotify.mock.calls[0]
    expect(notifiedRunId).toBe('run-3')
    expect(notifiedEvent).toBe('ingest.failed')
  })

  it('tenant isolation: every DB write goes through tenantQuery, never a raw pool query', async () => {
    mockBuildIngestPrompt.mockReturnValue({ system: [], messages: [{ role: 'user', content: 'x' }] })
    mockIngestCV.mockResolvedValue({ content: [{ type: 'text', text: validResponseText }] })
    mockTenantQuery.mockResolvedValue({ rows: [] })

    const releasedClient = { release: vi.fn(), query: vi.fn() }
    mockPoolConnect.mockResolvedValue(releasedClient)

    const job = { data: { user_id: 'user-4', run_id: 'run-4', raw_cv: 'x' } }
    await handleIngestCV(job)

    // the pool-connected client is only used for notify(), never for a direct query in this handler
    expect(releasedClient.query).not.toHaveBeenCalled()
    expect(mockTenantQuery.mock.calls.length).toBeGreaterThan(0)
  })

  it('transitions the row to processing before the Claude call', async () => {
    mockBuildIngestPrompt.mockReturnValue({ system: [], messages: [{ role: 'user', content: 'x' }] })
    mockTenantQuery.mockResolvedValue({ rows: [] })

    let processingCalledBeforeAnthropic = false
    mockTenantQuery.mockImplementation(async (_userId, sql) => {
      if (typeof sql === 'string' && sql.toLowerCase().includes('processing')) {
        processingCalledBeforeAnthropic = mockIngestCV.mock.calls.length === 0
      }
      return { rows: [] }
    })
    mockIngestCV.mockResolvedValue({ content: [{ type: 'text', text: validResponseText }] })

    const job = { data: { user_id: 'user-5', run_id: 'run-5', raw_cv: 'x' } }
    await handleIngestCV(job)

    expect(processingCalledBeforeAnthropic).toBe(true)
  })
})
