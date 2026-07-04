import { vi, describe, it, expect, beforeEach } from 'vitest'

// The handler under test wires real adapters (AnthropicEvaluator,
// PgEvaluationRepository) into the EvaluateJob use case. Rather than mocking
// relative-path modules and counting tenantQuery calls (the old brittle
// approach — see PR2 design), we mock only the three external dependencies
// the adapters wrap (`tenantQuery`, `buildEvaluationPrompt`, `evaluate`) and
// assert through observable side effects: the same 4 tenantQuery writes the
// repository contract guarantees, with correct values. The 4-write
// order/shape contract itself is fully covered (and is the source of truth)
// in `tests/adapters/pg-evaluation-repository.test.mjs` — this test exists to
// prove the handler wires everything together correctly end-to-end.

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

    // Should have called tenantQuery 5 times (upsert app, delete stale report, insert report, upsert usage, update job)
    expect(mockTenantQuery).toHaveBeenCalledTimes(5)

    // applications INSERT carries the parsed overall score and no status note
    const appCall = mockTenantQuery.mock.calls[0]
    expect(appCall[1]).toContain('INSERT INTO applications')
    expect(appCall[2]).toEqual(['user-1', 'job-1', 4.1, null])

    // Usage upsert call should increment evaluations_count
    const usageCall = mockTenantQuery.mock.calls[3]
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

    // tenantQuery for: UPSERT applications (with parse_error), DELETE stale reports, INSERT reports, UPSERT usage, UPDATE jobs
    mockTenantQuery
      .mockResolvedValueOnce({ rows: [{ id: 'app-uuid' }] })
      .mockResolvedValueOnce({ rows: [] })
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
      call => typeof call[1] === 'string' && call[1].includes('INSERT INTO reports')
    )
    expect(reportInsertCall).toBeDefined()

    // applications INSERT carries the parse-error status note and null score
    const appCall = mockTenantQuery.mock.calls.find(
      call => typeof call[1] === 'string' && call[1].includes('applications')
    )
    expect(appCall[2]).toEqual(['user-1', 'job-1', null, 'Evaluation completed (parse error in blocks)'])
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
    const reportCall = calls.find(c => typeof c[1] === 'string' && c[1].includes('INSERT INTO reports'))
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
