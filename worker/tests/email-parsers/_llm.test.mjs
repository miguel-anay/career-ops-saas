import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest'
import { MAX_HTML_LENGTH } from '../../email-parsers/_shared.mjs'

const mockEvaluate = vi.fn()

vi.mock('../../lib/anthropic.mjs', () => ({
  evaluate: mockEvaluate,
}))

const { parseEmailWithLLM } = await import('../../email-parsers/_llm.mjs')

describe('parseEmailWithLLM', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('caps oversized html before building the prompt (bounded LLM token cost)', async () => {
    mockEvaluate.mockResolvedValue({ content: [{ type: 'text', text: '[]' }] })
    const oversizedHtml = 'a'.repeat(MAX_HTML_LENGTH + 10_000)

    await parseEmailWithLLM({ subject: 's', html: oversizedHtml, text: '' })

    expect(mockEvaluate).toHaveBeenCalledTimes(1)
    const [, userContent] = mockEvaluate.mock.calls[0]
    expect(userContent.length).toBeLessThanOrEqual(MAX_HTML_LENGTH + 200) // + subject/prefix overhead
  })

  it('calls evaluate once and returns the parsed job array from the LLM response', async () => {
    mockEvaluate.mockResolvedValue({
      content: [{ type: 'text', text: '[{"title":"Backend Engineer","company":"Acme","url":"https://www.linkedin.com/jobs/view/123"}]' }],
    })

    const result = await parseEmailWithLLM({ subject: 'New jobs for you', html: '<p>...</p>', text: '' })

    expect(mockEvaluate).toHaveBeenCalledTimes(1)
    expect(result).toEqual([{ title: 'Backend Engineer', company: 'Acme', url: 'https://www.linkedin.com/jobs/view/123' }])
  })

  it('returns [] when the LLM response is not valid JSON', async () => {
    mockEvaluate.mockResolvedValue({ content: [{ type: 'text', text: 'not json' }] })

    const result = await parseEmailWithLLM({ subject: 's', html: '', text: '' })

    expect(result).toEqual([])
  })

  it('returns [] when the LLM response is valid JSON but not an array', async () => {
    mockEvaluate.mockResolvedValue({ content: [{ type: 'text', text: '{"title":"not an array"}' }] })

    const result = await parseEmailWithLLM({ subject: 's', html: '', text: '' })

    expect(result).toEqual([])
  })
})

describe('EMAIL_PARSER_LLM_FALLBACK gating in ingest-email.mjs', () => {
  const ORIGINAL_FLAG = process.env.EMAIL_PARSER_LLM_FALLBACK

  afterEach(() => {
    if (ORIGINAL_FLAG === undefined) delete process.env.EMAIL_PARSER_LLM_FALLBACK
    else process.env.EMAIL_PARSER_LLM_FALLBACK = ORIGINAL_FLAG
    vi.resetModules()
  })

  it('flag unset: a matched sender with 0 parsed jobs makes zero LLM calls', async () => {
    delete process.env.EMAIL_PARSER_LLM_FALLBACK

    vi.resetModules()
    const mockTenantQuery = vi.fn()
    const mockNotify = vi.fn()
    const mockPool = { connect: vi.fn() }
    const mockPoolClient = { query: vi.fn(), release: vi.fn() }
    const mockGetAccessToken = vi.fn()
    const mockListMessages = vi.fn()
    const mockGetMessage = vi.fn()
    const mockDecodeMessage = vi.fn()
    const mockFindParserForSender = vi.fn()
    const mockAllSenders = vi.fn(() => ['jobalerts-noreply@linkedin.com'])
    const llmSpy = vi.fn()

    vi.doMock('../../lib/db.mjs', () => ({ tenantQuery: mockTenantQuery, pool: mockPool }))
    vi.doMock('../../lib/progress.mjs', () => ({ notify: mockNotify }))
    vi.doMock('../../lib/gmail.mjs', () => ({
      getAccessToken: mockGetAccessToken,
      listMessages: mockListMessages,
      getMessage: mockGetMessage,
      decodeMessage: mockDecodeMessage,
    }))
    vi.doMock('../../email-parsers/index.mjs', () => ({
      allSenders: mockAllSenders,
      findParserForSender: mockFindParserForSender,
    }))
    vi.doMock('../../email-parsers/_llm.mjs', () => ({ parseEmailWithLLM: llmSpy }))

    mockPool.connect.mockResolvedValue(mockPoolClient)
    mockNotify.mockResolvedValue(undefined)
    mockTenantQuery
      .mockResolvedValueOnce({ rows: [{ google_refresh_token: 'token' }] }) // SELECT
      .mockResolvedValueOnce({ rows: [] }) // status update (no new jobs, no errors)
    mockGetAccessToken.mockResolvedValue('access-token')
    mockListMessages.mockResolvedValue([{ id: 'm1' }])
    mockGetMessage.mockResolvedValue({ id: 'm1' })
    mockDecodeMessage.mockReturnValue({ from: 'jobalerts-noreply@linkedin.com', subject: 'jobs', html: '', text: '' })
    mockFindParserForSender.mockReturnValue({ id: 'linkedin', parse: () => [] })

    const { handleIngestEmail } = await import('../../jobs/ingest-email.mjs')
    await handleIngestEmail({ data: { user_id: 'user-1', ingest_run_id: 'run-1' } })

    expect(llmSpy).not.toHaveBeenCalled()
  })

  it('flag=true: a matched sender with 0 parsed jobs calls the LLM fallback exactly once and upserts its result', async () => {
    process.env.EMAIL_PARSER_LLM_FALLBACK = 'true'

    vi.resetModules()
    const mockTenantQuery = vi.fn()
    const mockNotify = vi.fn()
    const mockPool = { connect: vi.fn() }
    const mockPoolClient = { query: vi.fn(), release: vi.fn() }
    const mockGetAccessToken = vi.fn()
    const mockListMessages = vi.fn()
    const mockGetMessage = vi.fn()
    const mockDecodeMessage = vi.fn()
    const mockFindParserForSender = vi.fn()
    const mockAllSenders = vi.fn(() => ['jobalerts-noreply@linkedin.com'])
    const llmSpy = vi.fn().mockResolvedValue([
      { title: 'Backend Engineer', company: 'Acme', url: 'https://www.linkedin.com/jobs/view/999' },
    ])

    vi.doMock('../../lib/db.mjs', () => ({ tenantQuery: mockTenantQuery, pool: mockPool }))
    vi.doMock('../../lib/progress.mjs', () => ({ notify: mockNotify }))
    vi.doMock('../../lib/gmail.mjs', () => ({
      getAccessToken: mockGetAccessToken,
      listMessages: mockListMessages,
      getMessage: mockGetMessage,
      decodeMessage: mockDecodeMessage,
    }))
    vi.doMock('../../email-parsers/index.mjs', () => ({
      allSenders: mockAllSenders,
      findParserForSender: mockFindParserForSender,
    }))
    vi.doMock('../../email-parsers/_llm.mjs', () => ({ parseEmailWithLLM: llmSpy }))

    mockPool.connect.mockResolvedValue(mockPoolClient)
    mockNotify.mockResolvedValue(undefined)
    mockTenantQuery
      .mockResolvedValueOnce({ rows: [{ google_refresh_token: 'token' }] }) // SELECT
      .mockResolvedValueOnce({ rows: [{ id: 'job-1', is_new: true }] }) // upsert from LLM result
      .mockResolvedValueOnce({ rows: [] }) // status update
    mockGetAccessToken.mockResolvedValue('access-token')
    mockListMessages.mockResolvedValue([{ id: 'm1' }])
    mockGetMessage.mockResolvedValue({ id: 'm1' })
    mockDecodeMessage.mockReturnValue({ from: 'jobalerts-noreply@linkedin.com', subject: 'jobs', html: '', text: '' })
    mockFindParserForSender.mockReturnValue({ id: 'linkedin', parse: () => [] })

    const { handleIngestEmail } = await import('../../jobs/ingest-email.mjs')
    await handleIngestEmail({ data: { user_id: 'user-1', ingest_run_id: 'run-1' } })

    expect(llmSpy).toHaveBeenCalledTimes(1)
    const jobFoundCalls = mockNotify.mock.calls.filter((c) => c[2] === 'ingest.job_found')
    expect(jobFoundCalls).toHaveLength(1)
  })

  it('flag=true but parser already extracted jobs: the LLM is never called', async () => {
    process.env.EMAIL_PARSER_LLM_FALLBACK = 'true'

    vi.resetModules()
    const mockTenantQuery = vi.fn()
    const mockNotify = vi.fn()
    const mockPool = { connect: vi.fn() }
    const mockPoolClient = { query: vi.fn(), release: vi.fn() }
    const mockGetAccessToken = vi.fn()
    const mockListMessages = vi.fn()
    const mockGetMessage = vi.fn()
    const mockDecodeMessage = vi.fn()
    const mockFindParserForSender = vi.fn()
    const mockAllSenders = vi.fn(() => ['jobalerts-noreply@linkedin.com'])
    const llmSpy = vi.fn()

    vi.doMock('../../lib/db.mjs', () => ({ tenantQuery: mockTenantQuery, pool: mockPool }))
    vi.doMock('../../lib/progress.mjs', () => ({ notify: mockNotify }))
    vi.doMock('../../lib/gmail.mjs', () => ({
      getAccessToken: mockGetAccessToken,
      listMessages: mockListMessages,
      getMessage: mockGetMessage,
      decodeMessage: mockDecodeMessage,
    }))
    vi.doMock('../../email-parsers/index.mjs', () => ({
      allSenders: mockAllSenders,
      findParserForSender: mockFindParserForSender,
    }))
    vi.doMock('../../email-parsers/_llm.mjs', () => ({ parseEmailWithLLM: llmSpy }))

    mockPool.connect.mockResolvedValue(mockPoolClient)
    mockNotify.mockResolvedValue(undefined)
    mockTenantQuery
      .mockResolvedValueOnce({ rows: [{ google_refresh_token: 'token' }] }) // SELECT
      .mockResolvedValueOnce({ rows: [{ id: 'job-1', is_new: true }] }) // upsert from deterministic parser
      .mockResolvedValueOnce({ rows: [] }) // status update
    mockGetAccessToken.mockResolvedValue('access-token')
    mockListMessages.mockResolvedValue([{ id: 'm1' }])
    mockGetMessage.mockResolvedValue({ id: 'm1' })
    mockDecodeMessage.mockReturnValue({ from: 'jobalerts-noreply@linkedin.com', subject: 'jobs', html: '', text: '' })
    mockFindParserForSender.mockReturnValue({
      id: 'linkedin',
      parse: () => [{ title: 'Deterministic Job', company: 'Co', url: 'https://www.linkedin.com/jobs/view/1' }],
    })

    const { handleIngestEmail } = await import('../../jobs/ingest-email.mjs')
    await handleIngestEmail({ data: { user_id: 'user-1', ingest_run_id: 'run-1' } })

    expect(llmSpy).not.toHaveBeenCalled()
  })
})
