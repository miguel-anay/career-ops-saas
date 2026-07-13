import { vi, describe, it, expect, beforeEach } from 'vitest'

const mockTenantQuery = vi.fn()

vi.mock('../../lib/db.mjs', () => ({
  tenantQuery: mockTenantQuery,
}))

const { buildEvaluationPrompt, mergeProfile } = await import('../../lib/prompt.mjs')

describe('mergeProfile', () => {
  it('an override key wins over the raw profile_json value', () => {
    const profileJson = { target_roles: { primary: ['Backend Engineer'] }, narrative: 'x' }
    const profileOverrides = { target_roles: { primary: ['Staff Engineer'] } }

    const result = mergeProfile(profileJson, profileOverrides)

    expect(result.target_roles).toEqual({ primary: ['Staff Engineer'] })
  })

  it('non-overridden keys pass through unchanged', () => {
    const profileJson = { target_roles: ['Backend Engineer'], narrative: 'unchanged narrative' }
    const profileOverrides = { target_roles: ['Staff Engineer'] }

    const result = mergeProfile(profileJson, profileOverrides)

    expect(result.narrative).toBe('unchanged narrative')
  })

  it('handles both string and object inputs', () => {
    const objResult = mergeProfile({ a: 1 }, { b: 2 })
    expect(objResult).toEqual({ a: 1, b: 2 })

    const strResult = mergeProfile('{"a":1}', '{"b":2}')
    expect(strResult).toEqual({ a: 1, b: 2 })
  })

  it('handles empty/nil inputs without throwing', () => {
    expect(() => mergeProfile(null, null)).not.toThrow()
    expect(mergeProfile(null, null)).toEqual({})
    expect(mergeProfile(undefined, undefined)).toEqual({})
    expect(mergeProfile('', '')).toEqual({})
  })
})

describe('buildEvaluationPrompt', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('returns system array with 2 blocks, each with cache_control: ephemeral', async () => {
    const userId = 'user-uuid'
    const jobId = 'job-uuid'

    mockTenantQuery
      .mockResolvedValueOnce({
        rows: [{
          cv_markdown: '# My CV\nExperienced engineer...',
          profile_json: JSON.stringify({ target_roles: ['Senior Engineer'] }),
        }],
      })
      .mockResolvedValueOnce({
        rows: [{
          scraped_content: 'Senior Software Engineer at Acme Corp...',
          title: 'Senior Software Engineer',
          company: 'Acme Corp',
          url: 'https://acme.com/jobs/1',
        }],
      })

    const result = await buildEvaluationPrompt(userId, jobId, { tenantQuery: mockTenantQuery })

    expect(result).toHaveProperty('system')
    expect(result).toHaveProperty('messages')
    expect(Array.isArray(result.system)).toBe(true)
    expect(result.system).toHaveLength(2)
  })

  it('applies cache_control ephemeral to both system blocks', async () => {
    mockTenantQuery
      .mockResolvedValueOnce({
        rows: [{
          cv_markdown: '# CV',
          profile_json: '{}',
        }],
      })
      .mockResolvedValueOnce({
        rows: [{
          scraped_content: 'JD content',
          title: 'Engineer',
          company: 'Corp',
          url: 'https://corp.com/job',
        }],
      })

    const result = await buildEvaluationPrompt('uid', 'jid', { tenantQuery: mockTenantQuery })

    for (const block of result.system) {
      expect(block.type).toBe('text')
      expect(block.cache_control).toEqual({ type: 'ephemeral' })
    }
  })

  it('includes cv_markdown in second system block', async () => {
    const cvMarkdown = '# Santiago CV\nHead of AI...'
    mockTenantQuery
      .mockResolvedValueOnce({
        rows: [{
          cv_markdown: cvMarkdown,
          profile_json: '{"name":"Santiago"}',
        }],
      })
      .mockResolvedValueOnce({
        rows: [{
          scraped_content: 'Job description here',
          title: 'Lead AI',
          company: 'Big Co',
          url: 'https://bigco.com/jobs/1',
        }],
      })

    const result = await buildEvaluationPrompt('uid', 'jid', { tenantQuery: mockTenantQuery })

    const secondBlock = result.system[1]
    expect(secondBlock.text).toContain(cvMarkdown)
  })

  it('includes scraped job content in user message', async () => {
    const scrapedContent = 'We are looking for a Senior AI Engineer...'
    mockTenantQuery
      .mockResolvedValueOnce({
        rows: [{ cv_markdown: '# CV', profile_json: '{}' }],
      })
      .mockResolvedValueOnce({
        rows: [{
          scraped_content: scrapedContent,
          title: 'AI Engineer',
          company: 'AI Corp',
          url: 'https://aicorp.com/job',
        }],
      })

    const result = await buildEvaluationPrompt('uid', 'jid', { tenantQuery: mockTenantQuery })

    expect(result.messages).toHaveLength(1)
    expect(result.messages[0].role).toBe('user')
    expect(result.messages[0].content).toContain(scrapedContent)
  })

  it('includes the posting age in the user message when the job has a received_at', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-06-30T00:00:00.000Z'))

    mockTenantQuery
      .mockResolvedValueOnce({
        rows: [{ cv_markdown: '# CV', profile_json: '{}' }],
      })
      .mockResolvedValueOnce({
        rows: [{
          scraped_content: 'JD content',
          title: 'Engineer',
          company: 'Corp',
          url: 'https://corp.com/job',
          received_at: '2026-06-25T00:00:00.000Z',
        }],
      })

    const result = await buildEvaluationPrompt('uid', 'jid', { tenantQuery: mockTenantQuery })

    expect(result.messages[0].content).toMatch(/posted 5 days ago/)

    vi.useRealTimers()
  })

  it('includes STAR-mapping and negotiation-guidance instructions in the system prompt', async () => {
    mockTenantQuery
      .mockResolvedValueOnce({ rows: [{ cv_markdown: '# CV', profile_json: '{}' }] })
      .mockResolvedValueOnce({
        rows: [{ scraped_content: 'JD', title: 'Eng', company: 'Corp', url: 'https://c.com/j' }],
      })

    const result = await buildEvaluationPrompt('uid', 'jid', { tenantQuery: mockTenantQuery })
    const staticSystemPrompt = result.system[0].text

    expect(staticSystemPrompt).toMatch(/STAR/)
    expect(staticSystemPrompt.toLowerCase()).toContain('negotiation')
  })

  it('still requests exactly 7 blocks (A-G) with unchanged field names', async () => {
    mockTenantQuery
      .mockResolvedValueOnce({ rows: [{ cv_markdown: '# CV', profile_json: '{}' }] })
      .mockResolvedValueOnce({
        rows: [{ scraped_content: 'JD', title: 'Eng', company: 'Corp', url: 'https://c.com/j' }],
      })

    const result = await buildEvaluationPrompt('uid', 'jid', { tenantQuery: mockTenantQuery })
    const staticSystemPrompt = result.system[0].text

    const blockHeaders = staticSystemPrompt.match(/##\s+Block\s+[A-G]\s*[—–-]/g) || []
    expect(blockHeaders).toHaveLength(7)
    expect(staticSystemPrompt).toContain('Score: X.X/5')
    expect(staticSystemPrompt).toContain('Tier: 1-5')
  })

  it('reflects a manually-overridden target role over the raw profile_json value (R7)', async () => {
    mockTenantQuery
      .mockResolvedValueOnce({
        rows: [{
          cv_markdown: '# CV',
          profile_json: JSON.stringify({ target_roles: { primary: ['Backend Engineer'] } }),
          profile_overrides: JSON.stringify({ target_roles: { primary: ['Staff Engineer'] } }),
        }],
      })
      .mockResolvedValueOnce({
        rows: [{ scraped_content: 'JD', title: 'Eng', company: 'Corp', url: 'https://c.com/j' }],
      })

    const result = await buildEvaluationPrompt('uid', 'jid', { tenantQuery: mockTenantQuery })
    const cvAndProfileBlock = result.system[1].text

    expect(cvAndProfileBlock).toContain('Staff Engineer')
    expect(cvAndProfileBlock).not.toContain('Backend Engineer')
  })

  it('fetches user and job using tenantQuery with userId', async () => {
    const userId = 'specific-user-id'
    const jobId = 'specific-job-id'

    mockTenantQuery.mockResolvedValue({ rows: [{ cv_markdown: '# CV', profile_json: '{}', scraped_content: 'JD', title: 'Eng', company: 'Corp', url: 'https://c.com/j' }] })

    await buildEvaluationPrompt(userId, jobId, { tenantQuery: mockTenantQuery })

    // Both calls should use the userId for RLS
    expect(mockTenantQuery.mock.calls[0][0]).toBe(userId)
    expect(mockTenantQuery.mock.calls[1][0]).toBe(userId)
  })
})

describe('buildEvaluationPrompt — article digest block (3rd system entry)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  function mockUserAndJob() {
    mockTenantQuery
      .mockResolvedValueOnce({ rows: [{ cv_markdown: '# CV', profile_json: '{}' }] })
      .mockResolvedValueOnce({
        rows: [{ scraped_content: 'JD', title: 'Eng', company: 'Corp', url: 'https://c.com/j' }],
      })
  }

  it('omits the third block entirely when the user has zero digests', async () => {
    mockUserAndJob()
    mockTenantQuery.mockResolvedValueOnce({ rows: [] })

    const result = await buildEvaluationPrompt('uid', 'jid', { tenantQuery: mockTenantQuery })

    expect(result.system).toHaveLength(2)
  })

  it('includes entries under both caps, newest-first, with an ephemeral third block', async () => {
    mockUserAndJob()
    mockTenantQuery.mockResolvedValueOnce({
      rows: [
        { title: 'Newest Project', content_md: 'Recent proof point.' },
        { title: 'Older Project', content_md: 'Older proof point.' },
      ],
    })

    const result = await buildEvaluationPrompt('uid', 'jid', { tenantQuery: mockTenantQuery })

    expect(result.system).toHaveLength(3)
    const digestBlock = result.system[2]
    expect(digestBlock.type).toBe('text')
    expect(digestBlock.cache_control).toEqual({ type: 'ephemeral' })

    const newestIndex = digestBlock.text.indexOf('Newest Project')
    const olderIndex = digestBlock.text.indexOf('Older Project')
    expect(newestIndex).toBeGreaterThanOrEqual(0)
    expect(olderIndex).toBeGreaterThan(newestIndex)
  })

  it('truncates a single entry over 4000 chars with a "…[truncated]" marker', async () => {
    mockUserAndJob()
    const longContent = 'x'.repeat(5000)
    mockTenantQuery.mockResolvedValueOnce({
      rows: [{ title: 'Huge Entry', content_md: longContent }],
    })

    const result = await buildEvaluationPrompt('uid', 'jid', { tenantQuery: mockTenantQuery })

    const digestBlock = result.system[2].text
    expect(digestBlock).toContain('…[truncated]')
    expect(digestBlock).toContain('x'.repeat(4000))
    expect(digestBlock).not.toContain('x'.repeat(4001))
  })

  it('drops the whole entry (and every older one) once the running total would exceed 24000 chars', async () => {
    mockUserAndJob()
    // Each entry (header + 4000-char capped body) renders to ~4012 chars.
    // 6 entries * ~4012 > 24000, so the 6th (oldest) entry must be dropped whole.
    const rows = Array.from({ length: 6 }, (_, i) => ({
      title: `Entry ${i}`,
      content_md: 'y'.repeat(4000),
    }))
    mockTenantQuery.mockResolvedValueOnce({ rows })

    const result = await buildEvaluationPrompt('uid', 'jid', { tenantQuery: mockTenantQuery })

    const digestBlock = result.system[2].text
    expect(digestBlock).toContain('Entry 0')
    expect(digestBlock).toContain('Entry 4')
    expect(digestBlock).not.toContain('Entry 5')
    expect(digestBlock.length).toBeLessThanOrEqual(24000 + 100)
  })
})
