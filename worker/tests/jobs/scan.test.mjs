import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest'

// Mock dependencies
const mockTenantQuery = vi.fn()
const mockNotify = vi.fn()
const mockPool = {
  connect: vi.fn(),
}
const mockPoolClient = {
  query: vi.fn(),
  release: vi.fn(),
}

vi.mock('../../lib/db.mjs', () => ({
  tenantQuery: mockTenantQuery,
  pool: mockPool,
}))

vi.mock('../../lib/progress.mjs', () => ({
  notify: mockNotify,
}))

// Mock providers — must mock the modules that scan.mjs imports dynamically
vi.mock('../../providers/greenhouse.mjs', () => ({
  default: {
    id: 'greenhouse',
    fetch: vi.fn().mockResolvedValue([]),
  },
}))

vi.mock('../../providers/ashby.mjs', () => ({
  default: { id: 'ashby', fetch: vi.fn().mockResolvedValue([]) },
}))

vi.mock('../../providers/lever.mjs', () => ({
  default: { id: 'lever', fetch: vi.fn().mockResolvedValue([]) },
}))

vi.mock('../../providers/recruitee.mjs', () => ({
  default: { id: 'recruitee', fetch: vi.fn().mockResolvedValue([]) },
}))

vi.mock('../../providers/smartrecruiters.mjs', () => ({
  default: { id: 'smartrecruiters', fetch: vi.fn().mockResolvedValue([]) },
}))

vi.mock('../../providers/workable.mjs', () => ({
  default: { id: 'workable', fetch: vi.fn().mockResolvedValue([]) },
}))

// Import after all mocks are set up
const scanModule = await import('../../jobs/scan.mjs')
const { handleScanCompany, finalizeScanRun } = scanModule

// Get provider mocks after import
const { default: mockGreenhouseProvider } = await import('../../providers/greenhouse.mjs')
const { default: mockWorkableProvider } = await import('../../providers/workable.mjs')

describe('handleScanCompany', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPool.connect.mockResolvedValue(mockPoolClient)
    mockPoolClient.query.mockResolvedValue({ rows: [] })
    mockNotify.mockResolvedValue(undefined)
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  it('happy path: provider returns jobs, UPSERT called, scan.job_found emitted for new jobs', async () => {
    const companyData = {
      id: 'company-1',
      name: 'Acme Corp',
      careers_url: 'https://boards.greenhouse.io/acme',
      provider_id: 'greenhouse',
      ats_api_url: null,
    }

    const foundJobs = [
      { title: 'Engineer', url: 'https://boards.greenhouse.io/acme/jobs/1', company: 'Acme Corp', location: 'Remote' },
      { title: 'Designer', url: 'https://boards.greenhouse.io/acme/jobs/2', company: 'Acme Corp', location: 'NYC' },
    ]

    mockGreenhouseProvider.fetch.mockResolvedValue(foundJobs)

    // tenantQuery call sequence:
    // 1) fetch company → returns companyData
    // 2) upsert job 1 → is_new = true
    // 3) upsert job 2 → is_new = false (no rows returned = conflict/no-op)
    // 4) update scan_runs new_jobs
    mockTenantQuery
      .mockResolvedValueOnce({ rows: [companyData] })
      .mockResolvedValueOnce({ rows: [{ id: 'job-uuid-1', is_new: true }] })
      .mockResolvedValueOnce({ rows: [] })  // second job: conflict, ON CONFLICT DO NOTHING
      .mockResolvedValueOnce({ rows: [] })  // update scan_runs

    const job = {
      data: {
        user_id: 'user-1',
        company_id: 'company-1',
        scan_run_id: 'scan-run-1',
      },
    }

    await handleScanCompany(job)

    // scan.job_found should be emitted for new jobs (is_new=true)
    const jobFoundCalls = mockNotify.mock.calls.filter(c => c[2] === 'scan.job_found')
    expect(jobFoundCalls.length).toBeGreaterThanOrEqual(1)

    // scan.company.done should be emitted
    const doneCalls = mockNotify.mock.calls.filter(c => c[2] === 'scan.company.done')
    expect(doneCalls).toHaveLength(1)
    expect(doneCalls[0][3]).toHaveProperty('company_id', 'company-1')
  })

  it('graceful degrade: provider throws → scan.company.error emitted, job does NOT rethrow (NFR-07)', async () => {
    const companyData = {
      id: 'company-workable',
      name: 'Workable Co',
      careers_url: 'https://apply.workable.com/workableco',
      provider_id: 'workable',
      ats_api_url: null,
    }

    // tenantQuery: 1) fetch company, 2) update scan_runs errors_json
    mockTenantQuery
      .mockResolvedValueOnce({ rows: [companyData] })
      .mockResolvedValueOnce({ rows: [] })  // update errors_json

    mockWorkableProvider.fetch.mockRejectedValue(new Error('Workable API timeout'))

    const job = {
      data: {
        user_id: 'user-1',
        company_id: 'company-workable',
        scan_run_id: 'scan-run-2',
      },
    }

    // Should NOT throw (NFR-07: graceful degrade)
    await expect(handleScanCompany(job)).resolves.not.toThrow()

    // scan.company.error should be emitted with correct company_id
    const errorCalls = mockNotify.mock.calls.filter(c => c[2] === 'scan.company.error')
    expect(errorCalls).toHaveLength(1)
    expect(errorCalls[0][3]).toHaveProperty('error')
    expect(errorCalls[0][3]).toHaveProperty('company_id', 'company-workable')
  })
})

describe('finalizeScanRun', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockNotify.mockResolvedValue(undefined)
    mockTenantQuery.mockResolvedValue({ rows: [] })
    mockPoolClient.query.mockResolvedValue({ rows: [] })
  })

  it('emits scan.completed with status completed when no errors', async () => {
    await finalizeScanRun('user-1', 'scan-run-1', { newJobs: 5, companiesOk: 2, errors: [] }, mockPoolClient)

    const completedCalls = mockNotify.mock.calls.filter(c => c[2] === 'scan.completed')
    expect(completedCalls).toHaveLength(1)
    expect(completedCalls[0][3].status).toBe('completed')
    expect(completedCalls[0][3].companies_ok).toBe(2)
    expect(completedCalls[0][3].companies_failed).toBe(0)
  })

  it('emits scan.completed with status partial when some errors present', async () => {
    await finalizeScanRun('user-1', 'scan-run-2', {
      newJobs: 3,
      companiesOk: 1,
      errors: [{ company_id: 'c1', error: 'Timeout' }],
    }, mockPoolClient)

    const completedCalls = mockNotify.mock.calls.filter(c => c[2] === 'scan.completed')
    expect(completedCalls).toHaveLength(1)
    expect(completedCalls[0][3].status).toBe('partial')
    expect(completedCalls[0][3].companies_ok).toBe(1)
    expect(completedCalls[0][3].companies_failed).toBe(1)
  })
})
