import { vi, describe, it, expect, beforeEach } from 'vitest'

const mockStart = vi.fn()
const mockRegisterWorker = vi.fn()
const mockHandleScanCompany = vi.fn()
const mockHandleEvaluateJob = vi.fn()
const mockHandleGeneratePDF = vi.fn()
const mockHandleIngestCV = vi.fn()
const mockHandleIngestEmail = vi.fn()
const mockHandleFetchJobContent = vi.fn()
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

vi.mock('../jobs/ingest-email.mjs', () => ({
  handleIngestEmail: mockHandleIngestEmail,
}))

vi.mock('../jobs/fetch-job-content.mjs', () => ({
  handleFetchJobContent: mockHandleFetchJobContent,
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

  it('registers the ingest-email job handler with teamSize 5', async () => {
    vi.resetModules()
    await import('../index.mjs')
    await new Promise(resolve => setTimeout(resolve, 0))

    const ingestEmailCall = mockRegisterWorker.mock.calls.find(call => call[0] === 'ingest-email')
    expect(ingestEmailCall).toBeDefined()
    expect(ingestEmailCall[1]).toBe(mockHandleIngestEmail)
    expect(ingestEmailCall[2]).toEqual({ teamSize: 5 })
  })

  it('registers the fetch-job-content job handler with teamSize 3', async () => {
    vi.resetModules()
    await import('../index.mjs')
    await new Promise(resolve => setTimeout(resolve, 0))

    const fetchJobContentCall = mockRegisterWorker.mock.calls.find(call => call[0] === 'fetch-job-content')
    expect(fetchJobContentCall).toBeDefined()
    expect(fetchJobContentCall[1]).toBe(mockHandleFetchJobContent)
    expect(fetchJobContentCall[2]).toEqual({ teamSize: 3 })
  })
})
