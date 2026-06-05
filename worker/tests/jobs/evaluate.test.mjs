import { vi, describe, it, expect, beforeEach } from 'vitest'

const mockTenantQuery = vi.fn()
const mockBuildEvaluationPrompt = vi.fn()
const mockAnthropicEvaluate = vi.fn()

vi.mock('../../lib/db.mjs', () => ({
  tenantQuery: mockTenantQuery,
}))

vi.mock('../../lib/prompt.mjs', () => ({
  buildEvaluationPrompt: mockBuildEvaluationPrompt,
}))

vi.mock('../../lib/anthropic.mjs', () => ({
  evaluate: mockAnthropicEvaluate,
}))

const { handleEvaluateJob } = await import('../../jobs/evaluate.mjs')

describe('handleEvaluateJob', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('happy path: Anthropic returns valid blocks → applications + reports inserted, usage updated', async () => {
    const validResponse = {
      content: [{
        type: 'text',
        text: `## Block A — Role & Company Fit
Score: 4.2/5
Strong alignment with AI engineering background.

## Block B — Technical Match
Score: 4.5/5
All required skills present.

## Block C — Compensation
Score: 3.8/5
Base salary within target range.

## Block D — Growth & Impact
Score: 4.0/5
Good growth trajectory.

## Block E — Culture & Location
Score: 3.5/5
Remote-first culture.

## Block F — Red Flags
Score: 4.5/5
No significant concerns.

## Block G — Posting Legitimacy
Tier: 1 — Verified Direct

**Overall Score: 4.1/5**`,
      }],
    }

    mockBuildEvaluationPrompt.mockResolvedValue({
      system: [],
      messages: [{ role: 'user', content: 'Evaluate this job' }],
    })

    mockAnthropicEvaluate.mockResolvedValue(validResponse)

    // tenantQuery mock for: INSERT applications, INSERT reports, UPSERT usage, UPDATE jobs
    mockTenantQuery
      .mockResolvedValueOnce({ rows: [{ id: 'app-uuid' }] })   // INSERT applications
      .mockResolvedValueOnce({ rows: [{ id: 'report-uuid' }] }) // INSERT reports
      .mockResolvedValueOnce({ rows: [] })                       // UPSERT usage
      .mockResolvedValueOnce({ rows: [] })                       // UPDATE jobs.status

    const job = {
      data: {
        user_id: 'user-1',
        job_id: 'job-1',
      },
    }

    await handleEvaluateJob(job)

    // Should have called tenantQuery 4 times (insert app, insert report, upsert usage, update job)
    expect(mockTenantQuery).toHaveBeenCalledTimes(4)

    // Usage upsert call should increment evaluations_count
    const usageCall = mockTenantQuery.mock.calls[2]
    expect(usageCall[1]).toContain('evaluations_count')
  })

  it('parse error guard: if parsing throws, persists parse_error:true and does not re-throw', async () => {
    const garbledResponse = {
      content: [{
        type: 'text',
        text: 'This is not a valid block format at all — just random text.',
      }],
    }

    mockBuildEvaluationPrompt.mockResolvedValue({
      system: [],
      messages: [{ role: 'user', content: 'Evaluate' }],
    })

    mockAnthropicEvaluate.mockResolvedValue(garbledResponse)

    // tenantQuery for: INSERT applications (with parse_error), INSERT reports, UPSERT usage, UPDATE jobs
    mockTenantQuery
      .mockResolvedValueOnce({ rows: [{ id: 'app-uuid' }] })
      .mockResolvedValueOnce({ rows: [{ id: 'report-uuid' }] })
      .mockResolvedValueOnce({ rows: [] })
      .mockResolvedValueOnce({ rows: [] })

    const job = {
      data: { user_id: 'user-1', job_id: 'job-1' },
    }

    // Should NOT throw even when parsing produces minimal blocks
    await expect(handleEvaluateJob(job)).resolves.not.toThrow()

    // Should still insert reports (with parse_error fallback)
    expect(mockTenantQuery).toHaveBeenCalled()
    // Find the reports insertion call and verify it has blocks_json
    const reportInsertCall = mockTenantQuery.mock.calls.find(
      call => typeof call[1] === 'string' && call[1].includes('reports')
    )
    expect(reportInsertCall).toBeDefined()
  })

  it('stores parse_error:true in blocks_json when response is unparseable', async () => {
    mockBuildEvaluationPrompt.mockResolvedValue({
      system: [],
      messages: [{ role: 'user', content: 'Evaluate' }],
    })
    mockAnthropicEvaluate.mockResolvedValue({
      content: [{ type: 'text', text: '' }],
    })

    mockTenantQuery.mockResolvedValue({ rows: [{ id: 'x' }] })

    const job = { data: { user_id: 'u1', job_id: 'j1' } }
    await handleEvaluateJob(job)

    // Find the call that inserts into reports and check blocks_json
    const calls = mockTenantQuery.mock.calls
    const reportCall = calls.find(c => typeof c[1] === 'string' && c[1].toLowerCase().includes('reports'))
    expect(reportCall).toBeDefined()

    // The blocks_json parameter should be present in the params array
    const params = reportCall[2]
    const blocksJsonParam = params.find(p => {
      if (typeof p === 'string') {
        try {
          const parsed = JSON.parse(p)
          return parsed.parse_error === true
        } catch {
          return false
        }
      }
      if (typeof p === 'object' && p !== null) {
        return p.parse_error === true
      }
      return false
    })
    expect(blocksJsonParam).toBeDefined()
  })
})
