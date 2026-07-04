import { describe, it, expect, vi, beforeEach } from 'vitest'

// pg-boss v10 invokes work() handlers with an ARRAY of jobs (batch), while
// every job handler in worker/jobs/ expects a single job object and
// destructures job.data. registerWorker must unwrap the batch — issue #42
// (every queue was failing 100% with "Cannot destructure property 'user_id'
// of 'job.data' as it is undefined").

const workMock = vi.fn()

vi.mock('pg-boss', () => ({
  default: vi.fn(() => ({
    start: vi.fn(),
    work: workMock,
  })),
}))

const { registerWorker } = await import('../../lib/queue.mjs')

describe('registerWorker (pg-boss v10 batch contract)', () => {
  beforeEach(() => {
    workMock.mockClear()
  })

  it('unwraps the v10 batch array: handler receives each single job', async () => {
    const handler = vi.fn()
    await registerWorker('evaluate-job', handler, { batchSize: 1 })

    expect(workMock).toHaveBeenCalledTimes(1)
    const wrapped = workMock.mock.calls[0][2]

    const job = { id: 'j1', data: { user_id: 'u1', job_id: 'x1' } }
    await wrapped([job])

    expect(handler).toHaveBeenCalledTimes(1)
    expect(handler).toHaveBeenCalledWith(job)
  })

  it('processes every job in a multi-job batch, in order', async () => {
    const seen = []
    const handler = vi.fn(async (job) => seen.push(job.id))
    await registerWorker('scan-company', handler)

    const wrapped = workMock.mock.calls[0][2]
    await wrapped([
      { id: 'a', data: {} },
      { id: 'b', data: {} },
    ])

    expect(seen).toEqual(['a', 'b'])
  })

  it('still works if pg-boss ever hands a single job object', async () => {
    const handler = vi.fn()
    await registerWorker('generate-pdf', handler)

    const wrapped = workMock.mock.calls[0][2]
    const job = { id: 'solo', data: { user_id: 'u1' } }
    await wrapped(job)

    expect(handler).toHaveBeenCalledWith(job)
  })
})
