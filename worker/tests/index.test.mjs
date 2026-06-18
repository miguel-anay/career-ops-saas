import { vi, describe, it, expect, beforeEach } from 'vitest'

const mockStart = vi.fn()
const mockRegisterWorker = vi.fn()
const mockHandleScanCompany = vi.fn()
const mockHandleEvaluateJob = vi.fn()
const mockHandleGeneratePDF = vi.fn()
const mockHandleIngestCV = vi.fn()
const mockListen = vi.fn()
const mockGet = vi.fn()

vi.mock('../lib/queue.mjs', () => ({
  start: mockStart,
  registerWorker: mockRegisterWorker,
}))

vi.mock('../jobs/scan.mjs', () => ({
  handleScanCompany: mockHandleScanCompany,
}))

vi.mock('../jobs/evaluate.mjs', () => ({
  handleEvaluateJob: mockHandleEvaluateJob,
}))

vi.mock('../jobs/pdf.mjs', () => ({
  handleGeneratePDF: mockHandleGeneratePDF,
}))

vi.mock('../jobs/ingest-cv.mjs', () => ({
  handleIngestCV: mockHandleIngestCV,
}))

vi.mock('express', () => ({
  default: () => ({
    get: mockGet,
    listen: mockListen,
  }),
}))

describe('worker index bootstrap', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockStart.mockResolvedValue(undefined)
    mockRegisterWorker.mockResolvedValue(undefined)
    mockListen.mockImplementation((_port, cb) => cb && cb())
  })

  it('registers the ingest-cv job handler with teamSize 5', async () => {
    vi.resetModules()
    await import('../index.mjs')
    // main() runs async — flush microtasks
    await new Promise(resolve => setTimeout(resolve, 0))

    const ingestCall = mockRegisterWorker.mock.calls.find(call => call[0] === 'ingest-cv')
    expect(ingestCall).toBeDefined()
    expect(ingestCall[1]).toBe(mockHandleIngestCV)
    expect(ingestCall[2]).toEqual({ teamSize: 5 })
  })
})
