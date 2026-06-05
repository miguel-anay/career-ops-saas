import { vi, describe, it, expect, beforeEach } from 'vitest'

const mockTenantQuery = vi.fn()
const mockRenderPDF = vi.fn()
const mockUploadBuffer = vi.fn()

vi.mock('../../lib/db.mjs', () => ({
  tenantQuery: mockTenantQuery,
}))

vi.mock('../../shared/generate-pdf.mjs', () => ({
  renderPDF: mockRenderPDF,
}))

vi.mock('../../lib/r2.mjs', () => ({
  uploadBuffer: mockUploadBuffer,
}))

const { handleGeneratePDF } = await import('../../jobs/pdf.mjs')

describe('handleGeneratePDF', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('happy path: fetches data, calls renderPDF, uploads to R2, updates pdf_path', async () => {
    const pdfBuffer = Buffer.from('%PDF-1.4 fake pdf content')
    mockRenderPDF.mockResolvedValue(pdfBuffer)
    mockUploadBuffer.mockResolvedValue({ key: 'user-1/job-1/cv.pdf', etag: '"abc123"' })

    // tenantQuery calls: fetch report, fetch job, fetch user CV, update applications
    mockTenantQuery
      .mockResolvedValueOnce({
        rows: [{
          id: 'report-1',
          content_md: '## Block A\nScore: 4.5/5',
          blocks_json: JSON.stringify({ blockA: { score: 4.5 } }),
        }],
      })
      .mockResolvedValueOnce({
        rows: [{
          id: 'job-1',
          title: 'Software Engineer',
          company: 'Acme Corp',
          url: 'https://acme.com/job',
        }],
      })
      .mockResolvedValueOnce({
        rows: [{
          cv_markdown: '# My CV\nExperienced engineer',
          profile_json: '{"name":"John"}',
        }],
      })
      .mockResolvedValueOnce({ rows: [] })  // UPDATE applications.pdf_path

    const job = {
      data: {
        user_id: 'user-1',
        job_id: 'job-1',
        application_id: 'app-1',
      },
    }

    await handleGeneratePDF(job)

    // renderPDF should have been called with HTML content
    expect(mockRenderPDF).toHaveBeenCalledOnce()
    const htmlArg = mockRenderPDF.mock.calls[0][0]
    expect(typeof htmlArg).toBe('string')
    expect(htmlArg.length).toBeGreaterThan(0)

    // R2 upload should use correct key pattern
    expect(mockUploadBuffer).toHaveBeenCalledOnce()
    const [key, buffer, mimeType] = mockUploadBuffer.mock.calls[0]
    expect(key).toBe('user-1/job-1/cv.pdf')
    expect(buffer).toBeInstanceOf(Buffer)
    expect(mimeType).toBe('application/pdf')

    // applications.pdf_path should be updated
    const updateCall = mockTenantQuery.mock.calls.find(
      c => typeof c[1] === 'string' && c[1].toLowerCase().includes('pdf_path')
    )
    expect(updateCall).toBeDefined()
  })

  it('uses correct R2 key format: {user_id}/{job_id}/cv.pdf', async () => {
    mockRenderPDF.mockResolvedValue(Buffer.from('pdf'))
    mockUploadBuffer.mockResolvedValue({ key: 'u2/j2/cv.pdf' })

    mockTenantQuery.mockResolvedValue({
      rows: [{
        id: 'x',
        content_md: 'content',
        blocks_json: '{}',
        title: 'Eng',
        company: 'Corp',
        url: 'https://c.com/j',
        cv_markdown: '# CV',
        profile_json: '{}',
      }],
    })

    const job = {
      data: { user_id: 'u2', job_id: 'j2', application_id: 'a2' },
    }

    await handleGeneratePDF(job)

    const uploadCall = mockUploadBuffer.mock.calls[0]
    expect(uploadCall[0]).toBe('u2/j2/cv.pdf')
  })

  it('updates applications.pdf_path with the R2 key after upload', async () => {
    const expectedKey = 'user-x/job-y/cv.pdf'
    mockRenderPDF.mockResolvedValue(Buffer.from('pdf'))
    mockUploadBuffer.mockResolvedValue({ key: expectedKey })

    mockTenantQuery.mockResolvedValue({
      rows: [{
        id: 'x',
        content_md: 'content',
        blocks_json: '{}',
        title: 'Eng',
        company: 'Corp',
        url: 'https://c.com/j',
        cv_markdown: '# CV',
        profile_json: '{}',
      }],
    })

    const job = { data: { user_id: 'user-x', job_id: 'job-y', application_id: 'app-z' } }
    await handleGeneratePDF(job)

    const updateCall = mockTenantQuery.mock.calls.find(
      c => typeof c[1] === 'string' && c[1].includes('pdf_path')
    )
    expect(updateCall).toBeDefined()
    // The key should be in the params
    const params = updateCall[2]
    expect(params).toContain(expectedKey)
  })
})
