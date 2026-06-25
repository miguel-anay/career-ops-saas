import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { PgEvaluationRepository } from '../../adapters/PgEvaluationRepository.mjs'
import { Evaluation } from '../../domain/Evaluation.mjs'

describe('PgEvaluationRepository.save', () => {
  beforeEach(() => {
    // T-176: freeze system time so the YYYY-MM month assertion can't flake
    // across a month boundary.
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-06-25T12:00:00.000Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('happy path: writes applications, reports, usage, jobs in order with correct values', async () => {
    const fakeTenantQuery = vi
      .fn()
      .mockResolvedValueOnce({ rows: [{ id: 'app-uuid' }] }) // INSERT applications
      .mockResolvedValueOnce({ rows: [{ id: 'report-uuid' }] }) // INSERT reports
      .mockResolvedValueOnce({ rows: [] }) // UPSERT usage
      .mockResolvedValueOnce({ rows: [] }) // UPDATE jobs

    const repository = new PgEvaluationRepository({ tenantQuery: fakeTenantQuery })

    const blocks = { blockA: { title: 'Role Fit', content: 'Strong', score: 4.2 } }
    const evaluation = Evaluation.fromBlocks(blocks, 4.1, 'raw markdown content')

    await repository.save('user-1', 'job-1', evaluation)

    expect(fakeTenantQuery).toHaveBeenCalledTimes(4)

    // 1. INSERT applications
    const [appUserId, appSql, appParams] = fakeTenantQuery.mock.calls[0]
    expect(appUserId).toBe('user-1')
    expect(appSql).toContain('INSERT INTO applications')
    expect(appSql).toContain("'Evaluated'")
    expect(appParams).toEqual(['user-1', 'job-1', 4.1, null])

    // 2. INSERT reports
    const [reportUserId, reportSql, reportParams] = fakeTenantQuery.mock.calls[1]
    expect(reportUserId).toBe('user-1')
    expect(reportSql).toContain('INSERT INTO reports')
    expect(reportParams[0]).toBe('user-1')
    expect(reportParams[1]).toBe('app-uuid')
    expect(reportParams[2]).toBe('raw markdown content')
    expect(JSON.parse(reportParams[3])).toEqual(blocks)

    // 3. UPSERT usage
    const [usageUserId, usageSql, usageParams] = fakeTenantQuery.mock.calls[2]
    expect(usageUserId).toBe('user-1')
    expect(usageSql).toContain('INSERT INTO usage')
    expect(usageSql).toContain('evaluations_count')
    expect(usageSql).toContain('ON CONFLICT')
    expect(usageParams).toEqual(['user-1', '2026-06'])

    // 4. UPDATE jobs
    const [jobsUserId, jobsSql, jobsParams] = fakeTenantQuery.mock.calls[3]
    expect(jobsUserId).toBe('user-1')
    expect(jobsSql).toContain("UPDATE jobs SET status = 'evaluated'")
    expect(jobsParams).toEqual(['job-1', 'user-1'])
  })

  it('parse-error path: writes applications with statusNote and reports with parse_error blocks_json', async () => {
    const fakeTenantQuery = vi
      .fn()
      .mockResolvedValueOnce({ rows: [{ id: 'app-uuid-2' }] })
      .mockResolvedValueOnce({ rows: [{ id: 'report-uuid-2' }] })
      .mockResolvedValueOnce({ rows: [] })
      .mockResolvedValueOnce({ rows: [] })

    const repository = new PgEvaluationRepository({ tenantQuery: fakeTenantQuery })

    const evaluation = Evaluation.parseError('garbled text')

    await repository.save('user-2', 'job-2', evaluation)

    expect(fakeTenantQuery).toHaveBeenCalledTimes(4)

    const [, , appParams] = fakeTenantQuery.mock.calls[0]
    expect(appParams).toEqual(['user-2', 'job-2', null, 'Evaluation completed (parse error in blocks)'])

    const [, , reportParams] = fakeTenantQuery.mock.calls[1]
    expect(reportParams[2]).toBe('garbled text')
    expect(JSON.parse(reportParams[3])).toEqual({ parse_error: true, raw: 'garbled text' })
  })
})
