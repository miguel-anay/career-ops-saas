import { vi, describe, it, expect, beforeEach } from 'vitest'

const mockTenantQuery = vi.fn()
const mockNotify = vi.fn()
const mockPool = { connect: vi.fn() }
const mockPoolClient = { query: vi.fn(), release: vi.fn() }

const mockGetAccessToken = vi.fn()
const mockListMessages = vi.fn()
const mockGetMessage = vi.fn()
const mockDecodeMessage = vi.fn()

const mockFindParserForSender = vi.fn()
const mockAllSenders = vi.fn()
const mockBossSend = vi.fn()

vi.mock('../../lib/db.mjs', () => ({
  tenantQuery: mockTenantQuery,
  pool: mockPool,
}))

vi.mock('../../lib/queue.mjs', () => ({
  default: { send: mockBossSend },
}))

vi.mock('../../lib/progress.mjs', () => ({
  notify: mockNotify,
}))

vi.mock('../../lib/gmail.mjs', () => ({
  getAccessToken: mockGetAccessToken,
  listMessages: mockListMessages,
  getMessage: mockGetMessage,
  decodeMessage: mockDecodeMessage,
}))

vi.mock('../../email-parsers/index.mjs', () => ({
  allSenders: mockAllSenders,
  findParserForSender: mockFindParserForSender,
}))

const { handleIngestEmail } = await import('../../jobs/ingest-email.mjs')

function baseJob() {
  return { data: { user_id: 'user-1', ingest_run_id: 'run-1' } }
}

describe('handleIngestEmail', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPool.connect.mockResolvedValue(mockPoolClient)
    mockNotify.mockResolvedValue(undefined)
    mockAllSenders.mockReturnValue(['jobalerts-noreply@linkedin.com', 'alert@indeed.com'])
    mockBossSend.mockResolvedValue(undefined)
  })

  it('null refresh token: marks run error, notifies, and makes no Gmail calls', async () => {
    mockTenantQuery
      .mockResolvedValueOnce({ rows: [{ google_refresh_token: null }] }) // SELECT
      .mockResolvedValueOnce({ rows: [] }) // errors_json update
      .mockResolvedValueOnce({ rows: [] }) // status update

    await handleIngestEmail(baseJob())

    expect(mockGetAccessToken).not.toHaveBeenCalled()
    expect(mockListMessages).not.toHaveBeenCalled()

    const statusUpdateCall = mockTenantQuery.mock.calls.find((c) => c[1].includes('SET status'))
    expect(statusUpdateCall[2]).toContain('error')

    const completedCalls = mockNotify.mock.calls.filter((c) => c[2] === 'ingest.completed')
    expect(completedCalls).toHaveLength(1)
    expect(completedCalls[0][3].status).toBe('error')
  })

  it('revoked token: getAccessToken throws -> run error with token_revoked, no listMessages call', async () => {
    mockTenantQuery
      .mockResolvedValueOnce({ rows: [{ google_refresh_token: 'stored-refresh-token' }] }) // SELECT
      .mockResolvedValueOnce({ rows: [] }) // errors_json update
      .mockResolvedValueOnce({ rows: [] }) // status update

    mockGetAccessToken.mockRejectedValue(new Error('invalid_grant'))

    await handleIngestEmail(baseJob())

    expect(mockListMessages).not.toHaveBeenCalled()

    const errorsUpdateCall = mockTenantQuery.mock.calls.find((c) => c[1].includes('errors_json'))
    expect(JSON.stringify(errorsUpdateCall[2])).toContain('token_revoked')

    const statusUpdateCall = mockTenantQuery.mock.calls.find((c) => c[1].includes('SET status'))
    expect(statusUpdateCall[2]).toContain('error')
  })

  it('2 new + 1 duplicate + 1 parse-error -> status partial with correct new_jobs count', async () => {
    mockTenantQuery
      .mockResolvedValueOnce({ rows: [{ google_refresh_token: 'stored-refresh-token' }] }) // SELECT
      .mockResolvedValueOnce({ rows: [{ id: 'job-1', is_new: true }] }) // upsert m1 -> new
      .mockResolvedValueOnce({ rows: [{ id: 'job-2', is_new: true }] }) // upsert m2 -> new
      .mockResolvedValueOnce({ rows: [] }) // upsert m3 -> duplicate (ON CONFLICT DO NOTHING)
      .mockResolvedValueOnce({ rows: [] }) // errors_json update (1 parse-error)
      .mockResolvedValueOnce({ rows: [] }) // status update

    mockGetAccessToken.mockResolvedValue('access-token')
    mockListMessages.mockResolvedValue([{ id: 'm1' }, { id: 'm2' }, { id: 'm3' }, { id: 'm4' }])
    mockGetMessage.mockImplementation(async (_token, id) => ({ id }))
    mockDecodeMessage.mockImplementation((msg) => ({
      from: 'jobalerts-noreply@linkedin.com',
      subject: msg.id,
      html: '',
      text: '',
    }))

    const jobsBySubject = {
      m1: [{ title: 'Job A', company: 'Co A', url: 'https://www.linkedin.com/jobs/view/1' }],
      m2: [{ title: 'Job B', company: 'Co B', url: 'https://www.linkedin.com/jobs/view/2' }],
      m3: [{ title: 'Job C', company: 'Co C', url: 'https://www.linkedin.com/jobs/view/3' }],
      m4: [{ title: 'Job D', company: 'Co D', url: 'not-a-valid-url' }], // triggers normalizeJobUrl -> null
    }
    mockFindParserForSender.mockReturnValue({
      id: 'linkedin',
      parse: (decoded) => jobsBySubject[decoded.subject],
    })

    await handleIngestEmail(baseJob())

    const statusUpdateCall = mockTenantQuery.mock.calls.find((c) => c[1].includes('SET status'))
    expect(statusUpdateCall[2]).toContain('partial')
    expect(statusUpdateCall[2]).toContain(2) // new_jobs = 2

    const jobFoundCalls = mockNotify.mock.calls.filter((c) => c[2] === 'ingest.job_found')
    expect(jobFoundCalls).toHaveLength(2)
  })

  it('all messages parse and upsert cleanly -> status completed', async () => {
    mockTenantQuery
      .mockResolvedValueOnce({ rows: [{ google_refresh_token: 'stored-refresh-token' }] }) // SELECT
      .mockResolvedValueOnce({ rows: [{ id: 'job-1', is_new: true }] }) // upsert m1 -> new
      .mockResolvedValueOnce({ rows: [] }) // status update (no errors -> no errors_json update)

    mockGetAccessToken.mockResolvedValue('access-token')
    mockListMessages.mockResolvedValue([{ id: 'm1' }])
    mockGetMessage.mockResolvedValue({ id: 'm1' })
    mockDecodeMessage.mockReturnValue({ from: 'alert@indeed.com', subject: 'jobs', html: '', text: '' })
    mockFindParserForSender.mockReturnValue({
      id: 'indeed',
      parse: () => [{ title: 'Job X', company: 'Co X', url: 'https://www.indeed.com/rc/clk?jk=abc' }],
    })

    await handleIngestEmail(baseJob())

    const statusUpdateCall = mockTenantQuery.mock.calls.find((c) => c[1].includes('SET status'))
    expect(statusUpdateCall[2]).toContain('completed')

    const completedCalls = mockNotify.mock.calls.filter((c) => c[2] === 'ingest.completed')
    expect(completedCalls[0][3].status).toBe('completed')
  })

  it('unrecognized sender is silently skipped (no parser match, run continues)', async () => {
    mockTenantQuery
      .mockResolvedValueOnce({ rows: [{ google_refresh_token: 'stored-refresh-token' }] }) // SELECT
      .mockResolvedValueOnce({ rows: [] }) // status update (no errors)

    mockGetAccessToken.mockResolvedValue('access-token')
    mockListMessages.mockResolvedValue([{ id: 'm1' }])
    mockGetMessage.mockResolvedValue({ id: 'm1' })
    mockDecodeMessage.mockReturnValue({ from: 'unknown@example.com', subject: 'spam', html: '', text: '' })
    mockFindParserForSender.mockReturnValue(null)

    await expect(handleIngestEmail(baseJob())).resolves.not.toThrow()

    const statusUpdateCall = mockTenantQuery.mock.calls.find((c) => c[1].includes('SET status'))
    expect(statusUpdateCall[2]).toContain('completed')
  })

  // FU-3: fetch-job-content enqueue coverage for the new-job path.
  describe('fetch-job-content enqueue', () => {
    function stubSingleNewJob() {
      mockTenantQuery
        .mockResolvedValueOnce({ rows: [{ google_refresh_token: 'stored-refresh-token' }] }) // SELECT
        .mockResolvedValueOnce({ rows: [{ id: 'job-1', is_new: true }] }) // upsert -> new
        .mockResolvedValueOnce({ rows: [] }) // status update

      mockGetAccessToken.mockResolvedValue('access-token')
      mockListMessages.mockResolvedValue([{ id: 'm1' }])
      mockGetMessage.mockResolvedValue({ id: 'm1' })
      mockDecodeMessage.mockReturnValue({ from: 'alert@indeed.com', subject: 'jobs', html: '', text: '' })
      mockFindParserForSender.mockReturnValue({
        id: 'indeed',
        parse: () => [{ title: 'Job X', company: 'Co X', url: 'https://www.indeed.com/rc/clk?jk=abc' }],
      })
    }

    it('enqueues fetch-job-content for a newly-inserted job', async () => {
      stubSingleNewJob()

      await handleIngestEmail(baseJob())

      expect(mockBossSend).toHaveBeenCalledTimes(1)
      expect(mockBossSend).toHaveBeenCalledWith('fetch-job-content', { user_id: 'user-1', job_id: 'job-1' })
    })

    it('does not enqueue fetch-job-content for a duplicate job (is_new false / zero rows)', async () => {
      mockTenantQuery
        .mockResolvedValueOnce({ rows: [{ google_refresh_token: 'stored-refresh-token' }] }) // SELECT
        .mockResolvedValueOnce({ rows: [] }) // upsert -> duplicate (ON CONFLICT DO NOTHING)
        .mockResolvedValueOnce({ rows: [] }) // status update

      mockGetAccessToken.mockResolvedValue('access-token')
      mockListMessages.mockResolvedValue([{ id: 'm1' }])
      mockGetMessage.mockResolvedValue({ id: 'm1' })
      mockDecodeMessage.mockReturnValue({ from: 'alert@indeed.com', subject: 'jobs', html: '', text: '' })
      mockFindParserForSender.mockReturnValue({
        id: 'indeed',
        parse: () => [{ title: 'Job X', company: 'Co X', url: 'https://www.indeed.com/rc/clk?jk=abc' }],
      })

      await handleIngestEmail(baseJob())

      expect(mockBossSend).not.toHaveBeenCalled()
    })

    it('boss.send throwing is caught and logged — the ingest run still completes', async () => {
      stubSingleNewJob()
      mockBossSend.mockRejectedValue(new Error('queue not registered'))

      await expect(handleIngestEmail(baseJob())).resolves.not.toThrow()

      expect(mockBossSend).toHaveBeenCalledTimes(1)
      const statusUpdateCall = mockTenantQuery.mock.calls.find((c) => c[1].includes('SET status'))
      expect(statusUpdateCall[2]).toContain('completed')
    })
  })
})
