import { vi, describe, it, expect, beforeEach } from 'vitest'

const mockTenantQuery = vi.fn()
const mockFetchPageText = vi.fn()
const mockIsHostAllowed = vi.fn()

vi.mock('../../lib/db.mjs', () => ({
  tenantQuery: mockTenantQuery,
}))

vi.mock('../../shared/fetch-page.mjs', () => ({
  fetchPageText: mockFetchPageText,
}))

vi.mock('../../lib/url-normalize.mjs', () => ({
  isHostAllowed: mockIsHostAllowed,
}))

const { handleFetchJobContent } = await import('../../jobs/fetch-job-content.mjs')

function baseJob(overrides = {}) {
  return { data: { user_id: 'user-1', job_id: 'job-1', ...overrides } }
}

describe('handleFetchJobContent', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('happy path: reads url, checks host, fetches text, writes scraped_content via tenantQuery', async () => {
    mockTenantQuery
      .mockResolvedValueOnce({ rows: [{ id: 'job-1', url: 'https://www.linkedin.com/jobs/view/1' }] }) // SELECT
      .mockResolvedValueOnce({ rows: [] }) // UPDATE

    mockIsHostAllowed.mockReturnValue(true)
    mockFetchPageText.mockResolvedValue('  Senior Engineer at Acme  ')

    await handleFetchJobContent(baseJob())

    expect(mockIsHostAllowed).toHaveBeenCalledWith('www.linkedin.com')
    expect(mockFetchPageText).toHaveBeenCalledWith('https://www.linkedin.com/jobs/view/1')

    const updateCall = mockTenantQuery.mock.calls.find(
      (c) => typeof c[1] === 'string' && c[1].toLowerCase().includes('update jobs')
    )
    expect(updateCall).toBeDefined()
    expect(updateCall[0]).toBe('user-1')
    expect(updateCall[2]).toEqual(['Senior Engineer at Acme', 'job-1'])
  })

  it('job not found: returns without calling isHostAllowed or fetchPageText', async () => {
    mockTenantQuery.mockResolvedValueOnce({ rows: [] }) // SELECT — zero rows

    await expect(handleFetchJobContent(baseJob())).resolves.not.toThrow()

    expect(mockIsHostAllowed).not.toHaveBeenCalled()
    expect(mockFetchPageText).not.toHaveBeenCalled()
    expect(mockTenantQuery).toHaveBeenCalledTimes(1) // only the SELECT, no UPDATE
  })

  it('unparseable url: returns without calling isHostAllowed or fetchPageText', async () => {
    mockTenantQuery.mockResolvedValueOnce({ rows: [{ id: 'job-1', url: 'not-a-valid-url' }] })

    await expect(handleFetchJobContent(baseJob())).resolves.not.toThrow()

    expect(mockIsHostAllowed).not.toHaveBeenCalled()
    expect(mockFetchPageText).not.toHaveBeenCalled()
    expect(mockTenantQuery).toHaveBeenCalledTimes(1)
  })

  it('disallowed host: returns without calling fetchPageText, scraped_content not written', async () => {
    mockTenantQuery.mockResolvedValueOnce({
      rows: [{ id: 'job-1', url: 'https://tracking.evil.com/jobs/1' }],
    })
    mockIsHostAllowed.mockReturnValue(false)

    await expect(handleFetchJobContent(baseJob())).resolves.not.toThrow()

    expect(mockIsHostAllowed).toHaveBeenCalledWith('tracking.evil.com')
    expect(mockFetchPageText).not.toHaveBeenCalled()
    expect(mockTenantQuery).toHaveBeenCalledTimes(1)
  })

  it('Playwright throws: logs and returns without writing scraped_content', async () => {
    mockTenantQuery.mockResolvedValueOnce({
      rows: [{ id: 'job-1', url: 'https://www.indeed.com/viewjob?jk=abc' }],
    })
    mockIsHostAllowed.mockReturnValue(true)
    mockFetchPageText.mockRejectedValue(new Error('net::ERR_TIMED_OUT'))

    await expect(handleFetchJobContent(baseJob())).resolves.not.toThrow()

    expect(mockTenantQuery).toHaveBeenCalledTimes(1) // no UPDATE call after the throw
  })

  it('empty/whitespace-only extraction: returns without writing scraped_content', async () => {
    mockTenantQuery.mockResolvedValueOnce({
      rows: [{ id: 'job-1', url: 'https://www.bumeran.com.pe/empleos/1.html' }],
    })
    mockIsHostAllowed.mockReturnValue(true)
    mockFetchPageText.mockResolvedValue('   \n  ')

    await expect(handleFetchJobContent(baseJob())).resolves.not.toThrow()

    expect(mockTenantQuery).toHaveBeenCalledTimes(1) // no UPDATE call
  })
})
