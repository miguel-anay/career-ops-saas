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

  it('happy path: upserts application, replaces report, upserts usage, updates job in order', async () => {
    const fakeTenantQuery = vi
      .fn()
      .mockResolvedValueOnce({ rows: [{ id: 'app-uuid' }] }) // UPSERT applications
      .mockResolvedValueOnce({ rows: [] }) // DELETE stale reports
      .mockResolvedValueOnce({ rows: [{ id: 'report-uuid' }] }) // INSERT reports
      .mockResolvedValueOnce({ rows: [] }) // UPSERT usage
      .mockResolvedValueOnce({ rows: [] }) // UPDATE jobs

    const repository = new PgEvaluationRepository({ tenantQuery: fakeTenantQuery })

    // blocks_json is persisted as an array of {label, content} (A→G order),
    // not the legacy letter-keyed object — see EvaluationParser's array flip
    // (evaluation-quality change). save() itself needs no shape-specific
    // code: JSON.stringify(evaluation.blocks) serializes whatever shape the
    // parser produced.
    const blocks = [{ label: 'Role Fit', content: 'Strong' }]
    const evaluation = Evaluation.fromBlocks(blocks, 4.1, 'raw markdown content')

    await repository.save('user-1', 'job-1', evaluation)

    expect(fakeTenantQuery).toHaveBeenCalledTimes(5)

    // 1. UPSERT applications — re-evaluating an already-evaluated job must not
    // violate applications_job_id_key (issue: duplicate key on re-evaluation)
    const [appUserId, appSql, appParams] = fakeTenantQuery.mock.calls[0]
    expect(appUserId).toBe('user-1')
    expect(appSql).toContain('INSERT INTO applications')
    expect(appSql).toContain('ON CONFLICT (job_id) DO UPDATE')
    expect(appSql).toContain("'Evaluated'")
    expect(appParams).toEqual(['user-1', 'job-1', 4.1, null])

    // 2. DELETE stale reports — GetReportByApplicationID is LIMIT 1 without
    // ORDER BY, so exactly one report per application may exist
    const [delUserId, delSql, delParams] = fakeTenantQuery.mock.calls[1]
    expect(delUserId).toBe('user-1')
    expect(delSql).toContain('DELETE FROM reports')
    expect(delParams).toEqual(['app-uuid', 'user-1'])

    // 3. INSERT reports
    const [reportUserId, reportSql, reportParams] = fakeTenantQuery.mock.calls[2]
    expect(reportUserId).toBe('user-1')
    expect(reportSql).toContain('INSERT INTO reports')
    expect(reportParams[0]).toBe('user-1')
    expect(reportParams[1]).toBe('app-uuid')
    expect(reportParams[2]).toBe('raw markdown content')
    expect(JSON.parse(reportParams[3])).toEqual(blocks)

    // 4. UPSERT usage
    const [usageUserId, usageSql, usageParams] = fakeTenantQuery.mock.calls[3]
    expect(usageUserId).toBe('user-1')
    expect(usageSql).toContain('INSERT INTO usage')
    expect(usageSql).toContain('evaluations_count')
    expect(usageSql).toContain('ON CONFLICT')
    expect(usageParams).toEqual(['user-1', '2026-06'])

    // 5. UPDATE jobs
    const [jobsUserId, jobsSql, jobsParams] = fakeTenantQuery.mock.calls[4]
    expect(jobsUserId).toBe('user-1')
    expect(jobsSql).toContain("UPDATE jobs SET status = 'evaluated'")
    expect(jobsParams).toEqual(['job-1', 'user-1'])
  })

  it('parse-error path: writes applications with statusNote and reports with parse_error blocks_json', async () => {
    const fakeTenantQuery = vi
      .fn()
      .mockResolvedValueOnce({ rows: [{ id: 'app-uuid-2' }] })
      .mockResolvedValueOnce({ rows: [] }) // DELETE stale reports
      .mockResolvedValueOnce({ rows: [{ id: 'report-uuid-2' }] })
      .mockResolvedValueOnce({ rows: [] })
      .mockResolvedValueOnce({ rows: [] })

    const repository = new PgEvaluationRepository({ tenantQuery: fakeTenantQuery })

    const evaluation = Evaluation.parseError('garbled text')

    await repository.save('user-2', 'job-2', evaluation)

    expect(fakeTenantQuery).toHaveBeenCalledTimes(5)

    const [, , appParams] = fakeTenantQuery.mock.calls[0]
    expect(appParams).toEqual(['user-2', 'job-2', null, 'Evaluation completed (parse error in blocks)'])

    const [, , reportParams] = fakeTenantQuery.mock.calls[2]
    expect(reportParams[2]).toBe('garbled text')
    expect(JSON.parse(reportParams[3])).toEqual({ parse_error: true, raw: 'garbled text' })
  })
})
