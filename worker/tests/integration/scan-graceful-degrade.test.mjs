/**
 * Integration test: SC-07 graceful degrade
 *
 * Scenario:
 *   - Workable provider throws an error ("404 Not Found")
 *   - Greenhouse provider returns 3 valid jobs
 *
 * Assertions:
 *   - scan.company.error NOTIFY sent for Workable company
 *   - 3 jobs UPSERTED from Greenhouse (tenantQuery called 3 times for upsert)
 *   - finalizeScanRun sets status to 'partial'
 *   - errors_json updated for the Workable error
 *   - Greenhouse results are NOT discarded
 */

import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest'

// ---------------------------------------------------------------------------
// Mock dependencies (must be declared before dynamic import of scan.mjs)
// ---------------------------------------------------------------------------

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

// Provider mocks — Workable will throw, Greenhouse will return 3 jobs
vi.mock('../../providers/greenhouse.mjs', () => ({
  default: {
    id: 'greenhouse',
    fetch: vi.fn(),
  },
}))

vi.mock('../../providers/workable.mjs', () => ({
  default: {
    id: 'workable',
    fetch: vi.fn(),
  },
}))

// Stub remaining providers (not under test here)
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

// Import handlers and provider mocks after all mocks are set up
const { handleScanCompany, finalizeScanRun } = await import('../../jobs/scan.mjs')
const { default: mockGreenhouseProvider } = await import('../../providers/greenhouse.mjs')
const { default: mockWorkableProvider } = await import('../../providers/workable.mjs')

// ---------------------------------------------------------------------------
// Shared fixtures
// ---------------------------------------------------------------------------

const GREENHOUSE_JOBS = [
  { title: 'Staff Engineer', url: 'https://boards.greenhouse.io/acme/jobs/1', company: 'Acme Corp', location: 'Remote' },
  { title: 'Senior Product Manager', url: 'https://boards.greenhouse.io/acme/jobs/2', company: 'Acme Corp', location: 'NYC' },
  { title: 'Design Lead', url: 'https://boards.greenhouse.io/acme/jobs/3', company: 'Acme Corp', location: 'Remote' },
]

const WORKABLE_COMPANY = {
  id: 'company-workable-001',
  name: 'Broken Workable Co',
  careers_url: 'https://apply.workable.com/broken',
  provider_id: 'workable',
  ats_api_url: null,
}

const GREENHOUSE_COMPANY = {
  id: 'company-greenhouse-001',
  name: 'Acme Corp',
  careers_url: 'https://boards.greenhouse.io/acme',
  provider_id: 'greenhouse',
  ats_api_url: null,
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('SC-07 graceful degrade: Workable fails, Greenhouse succeeds', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPool.connect.mockResolvedValue(mockPoolClient)
    mockPoolClient.query.mockResolvedValue({ rows: [] })
    mockPoolClient.release.mockResolvedValue(undefined)
    mockNotify.mockResolvedValue(undefined)
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  // -------------------------------------------------------------------------
  // Workable failure scenario
  // -------------------------------------------------------------------------

  it('Workable failure: scan.company.error NOTIFY sent and job does not rethrow', async () => {
    mockWorkableProvider.fetch.mockRejectedValue(new Error('404 Not Found'))

    mockTenantQuery
      .mockResolvedValueOnce({ rows: [WORKABLE_COMPANY] })   // fetch company
      .mockResolvedValueOnce({ rows: [] })                    // update errors_json

    const job = {
      data: {
        user_id: 'user-integration-001',
        company_id: WORKABLE_COMPANY.id,
        scan_run_id: 'scan-run-integration-001',
      },
    }

    // Must NOT throw (SC-07 / NFR-07: graceful degrade)
    await expect(handleScanCompany(job)).resolves.not.toThrow()

    // scan.company.error must be emitted for Workable
    const errorCalls = mockNotify.mock.calls.filter(c => c[2] === 'scan.company.error')
    expect(errorCalls).toHaveLength(1)
    expect(errorCalls[0][3]).toMatchObject({
      company_id: WORKABLE_COMPANY.id,
      error: expect.stringContaining('404 Not Found'),
    })
  })

  it('Workable failure: errors_json updated with Workable error details', async () => {
    mockWorkableProvider.fetch.mockRejectedValue(new Error('404 Not Found'))

    mockTenantQuery
      .mockResolvedValueOnce({ rows: [WORKABLE_COMPANY] })
      .mockResolvedValueOnce({ rows: [] })  // UPDATE scan_runs errors_json

    const job = {
      data: {
        user_id: 'user-integration-001',
        company_id: WORKABLE_COMPANY.id,
        scan_run_id: 'scan-run-integration-001',
      },
    }

    await handleScanCompany(job)

    // The tenantQuery call that updates errors_json must contain the error
    const updateCalls = mockTenantQuery.mock.calls.filter(
      ([, sql]) => typeof sql === 'string' && sql.includes('errors_json')
    )
    expect(updateCalls.length).toBeGreaterThanOrEqual(1)

    const errorPayload = updateCalls[0][2][0] // first param of UPDATE call
    const parsed = JSON.parse(errorPayload)
    expect(Array.isArray(parsed)).toBe(true)
    expect(parsed[0]).toMatchObject({
      company_id: WORKABLE_COMPANY.id,
      error: expect.stringContaining('404'),
    })
  })

  // -------------------------------------------------------------------------
  // Greenhouse success scenario
  // -------------------------------------------------------------------------

  it('Greenhouse success: 3 jobs upserted, scan.company.done emitted', async () => {
    mockGreenhouseProvider.fetch.mockResolvedValue(GREENHOUSE_JOBS)

    // tenantQuery sequence:
    //   1) fetch company
    //   2) upsert job 1 → is_new=true
    //   3) upsert job 2 → is_new=true
    //   4) upsert job 3 → is_new=true
    //   5) UPDATE scan_runs new_jobs
    mockTenantQuery
      .mockResolvedValueOnce({ rows: [GREENHOUSE_COMPANY] })
      .mockResolvedValueOnce({ rows: [{ id: 'job-uuid-1', is_new: true }] })
      .mockResolvedValueOnce({ rows: [{ id: 'job-uuid-2', is_new: true }] })
      .mockResolvedValueOnce({ rows: [{ id: 'job-uuid-3', is_new: true }] })
      .mockResolvedValueOnce({ rows: [] }) // UPDATE new_jobs

    const job = {
      data: {
        user_id: 'user-integration-001',
        company_id: GREENHOUSE_COMPANY.id,
        scan_run_id: 'scan-run-integration-002',
      },
    }

    await handleScanCompany(job)

    // 3 jobs should have been upserted
    const upsertCalls = mockTenantQuery.mock.calls.filter(
      ([, sql]) => typeof sql === 'string' && sql.includes('INSERT INTO jobs')
    )
    expect(upsertCalls).toHaveLength(3)

    // scan.company.done must be emitted
    const doneCalls = mockNotify.mock.calls.filter(c => c[2] === 'scan.company.done')
    expect(doneCalls).toHaveLength(1)
    expect(doneCalls[0][3]).toMatchObject({
      company_id: GREENHOUSE_COMPANY.id,
      found: 3,
      new: 3,
    })
  })

  it('Greenhouse success: scan.job_found emitted for each new job', async () => {
    mockGreenhouseProvider.fetch.mockResolvedValue(GREENHOUSE_JOBS)

    mockTenantQuery
      .mockResolvedValueOnce({ rows: [GREENHOUSE_COMPANY] })
      .mockResolvedValueOnce({ rows: [{ id: 'job-uuid-1', is_new: true }] })
      .mockResolvedValueOnce({ rows: [{ id: 'job-uuid-2', is_new: true }] })
      .mockResolvedValueOnce({ rows: [{ id: 'job-uuid-3', is_new: true }] })
      .mockResolvedValueOnce({ rows: [] })

    const job = {
      data: {
        user_id: 'user-integration-001',
        company_id: GREENHOUSE_COMPANY.id,
        scan_run_id: 'scan-run-integration-002',
      },
    }

    await handleScanCompany(job)

    const jobFoundCalls = mockNotify.mock.calls.filter(c => c[2] === 'scan.job_found')
    expect(jobFoundCalls).toHaveLength(3)
    expect(jobFoundCalls[0][3]).toMatchObject({ is_new: true })
  })

  // -------------------------------------------------------------------------
  // finalizeScanRun: partial status when errors present
  // -------------------------------------------------------------------------

  it('finalizeScanRun sets status to partial when Workable error is in summary', async () => {
    mockTenantQuery.mockResolvedValue({ rows: [] })

    const summary = {
      newJobs: 3, // Greenhouse jobs made it through
      errors: [
        {
          company_id: WORKABLE_COMPANY.id,
          company: WORKABLE_COMPANY.name,
          provider: 'workable',
          error: '404 Not Found',
        },
      ],
    }

    await finalizeScanRun(
      'user-integration-001',
      'scan-run-integration-003',
      summary,
      mockPoolClient
    )

    // scan.completed must be emitted with status=partial
    const completedCalls = mockNotify.mock.calls.filter(c => c[2] === 'scan.completed')
    expect(completedCalls).toHaveLength(1)
    expect(completedCalls[0][3].status).toBe('partial')
    expect(completedCalls[0][3].new_jobs).toBe(3)
    expect(completedCalls[0][3].companies_failed).toBe(1)
  })

  it('finalizeScanRun sets status to completed when no errors', async () => {
    mockTenantQuery.mockResolvedValue({ rows: [] })

    const summary = { newJobs: 3, errors: [] }

    await finalizeScanRun(
      'user-integration-001',
      'scan-run-integration-004',
      summary,
      mockPoolClient
    )

    const completedCalls = mockNotify.mock.calls.filter(c => c[2] === 'scan.completed')
    expect(completedCalls).toHaveLength(1)
    expect(completedCalls[0][3].status).toBe('completed')
  })

  // -------------------------------------------------------------------------
  // Greenhouse results NOT discarded when Workable fails (separate jobs)
  // -------------------------------------------------------------------------

  it('Greenhouse results are NOT discarded when a separate Workable scan fails', async () => {
    // This simulates two sequential scan-company jobs in a single scan run:
    //   Job 1: Workable → fails (graceful degrade)
    //   Job 2: Greenhouse → succeeds, 3 jobs found
    // The integration point is that finalizeScanRun receives newJobs=3 (not 0).

    mockWorkableProvider.fetch.mockRejectedValue(new Error('404 Not Found'))
    mockGreenhouseProvider.fetch.mockResolvedValue(GREENHOUSE_JOBS)

    // Shared state that would be accumulated by pg-boss across two jobs
    let accumulatedErrors = []
    let accumulatedNewJobs = 0

    // ---- Workable job ----
    mockTenantQuery
      .mockResolvedValueOnce({ rows: [WORKABLE_COMPANY] })
      .mockResolvedValueOnce({ rows: [] })  // errors_json update

    await handleScanCompany({
      data: { user_id: 'user-x', company_id: WORKABLE_COMPANY.id, scan_run_id: 'scan-run-x' },
    })

    // Capture error from notify call
    const workableError = mockNotify.mock.calls.find(c => c[2] === 'scan.company.error')
    if (workableError) {
      accumulatedErrors.push({ company_id: WORKABLE_COMPANY.id, error: workableError[3].error })
    }

    vi.clearAllMocks()
    mockPool.connect.mockResolvedValue(mockPoolClient)
    mockPoolClient.release.mockResolvedValue(undefined)
    mockNotify.mockResolvedValue(undefined)

    // ---- Greenhouse job ----
    mockTenantQuery
      .mockResolvedValueOnce({ rows: [GREENHOUSE_COMPANY] })
      .mockResolvedValueOnce({ rows: [{ id: 'j1', is_new: true }] })
      .mockResolvedValueOnce({ rows: [{ id: 'j2', is_new: true }] })
      .mockResolvedValueOnce({ rows: [{ id: 'j3', is_new: true }] })
      .mockResolvedValueOnce({ rows: [] })

    await handleScanCompany({
      data: { user_id: 'user-x', company_id: GREENHOUSE_COMPANY.id, scan_run_id: 'scan-run-x' },
    })

    const doneCalls = mockNotify.mock.calls.filter(c => c[2] === 'scan.company.done')
    expect(doneCalls).toHaveLength(1)
    accumulatedNewJobs += doneCalls[0][3].new

    // ---- Finalize ----
    vi.clearAllMocks()
    mockTenantQuery.mockResolvedValue({ rows: [] })
    mockNotify.mockResolvedValue(undefined)

    await finalizeScanRun('user-x', 'scan-run-x', {
      newJobs: accumulatedNewJobs,
      errors: accumulatedErrors,
    }, mockPoolClient)

    const completedCalls = mockNotify.mock.calls.filter(c => c[2] === 'scan.completed')
    expect(completedCalls).toHaveLength(1)

    const result = completedCalls[0][3]
    expect(result.status).toBe('partial')      // partial because Workable failed
    expect(result.new_jobs).toBe(3)            // Greenhouse jobs preserved (SC-07)
    expect(result.companies_failed).toBe(1)    // Only Workable failed
  })
})
