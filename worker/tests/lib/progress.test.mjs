import { vi, describe, it, expect, beforeEach } from 'vitest'

const { notify } = await import('../../lib/progress.mjs')

describe('notify', () => {
  let mockClient

  beforeEach(() => {
    mockClient = {
      query: vi.fn().mockResolvedValue({ rows: [], rowCount: 0 }),
    }
  })

  it('calls pg_notify with correct channel and payload shape', async () => {
    await notify(mockClient, 'scan-run-123', 'scan.started', { total_companies: 5 })

    expect(mockClient.query).toHaveBeenCalledOnce()
    const [sql, params] = mockClient.query.mock.calls[0]
    expect(sql).toContain('pg_notify')
    expect(sql).toContain('scan_progress')
    expect(params).toHaveLength(1)
  })

  it('payload contains event, run_id, ts, and data fields', async () => {
    await notify(mockClient, 'run-456', 'scan.job_found', { job_id: 'j1', title: 'Engineer' })

    const [, params] = mockClient.query.mock.calls[0]
    const payload = JSON.parse(params[0])

    expect(payload).toHaveProperty('event', 'scan.job_found')
    expect(payload).toHaveProperty('run_id', 'run-456')
    expect(payload).toHaveProperty('ts')
    expect(payload).toHaveProperty('data')
    expect(payload.data).toEqual({ job_id: 'j1', title: 'Engineer' })
  })

  it('ts is an ISO 8601 timestamp string', async () => {
    await notify(mockClient, 'run-789', 'scan.completed', { status: 'completed' })

    const [, params] = mockClient.query.mock.calls[0]
    const payload = JSON.parse(params[0])

    expect(typeof payload.ts).toBe('string')
    expect(() => new Date(payload.ts)).not.toThrow()
    expect(new Date(payload.ts).toISOString()).toBe(payload.ts)
  })

  it('works with scan.company.error event', async () => {
    await notify(mockClient, 'run-1', 'scan.company.error', {
      company_id: 'c1',
      company: 'Acme',
      provider: 'workable',
      error: 'Timeout after 10s',
    })

    const [, params] = mockClient.query.mock.calls[0]
    const payload = JSON.parse(params[0])

    expect(payload.event).toBe('scan.company.error')
    expect(payload.data.provider).toBe('workable')
  })
})
