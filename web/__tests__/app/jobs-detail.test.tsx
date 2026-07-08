import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, waitFor, fireEvent, cleanup } from '@testing-library/react'
import React from 'react'
import JobDetailPage from '../../app/jobs/[id]/page'

const { mockApiGet, mockApiPost, mockIsAuthenticated, mockRouter, mockParams } = vi.hoisted(() => ({
  mockApiGet: vi.fn(),
  mockApiPost: vi.fn(),
  mockIsAuthenticated: vi.fn(() => true),
  mockRouter: { push: vi.fn(), replace: vi.fn() },
  mockParams: { id: 'job-1' },
}))

vi.mock('next/navigation', () => ({
  useRouter: () => mockRouter,
  useParams: () => mockParams,
  redirect: vi.fn(),
}))

import { ApiError } from '../../lib/api'

vi.mock('../../lib/api', () => ({
  apiGet: mockApiGet,
  apiPost: mockApiPost,
  ApiError: class ApiError extends Error {
    status: number
    code: string | null
    constructor(status: number, code: string | null, message: string) {
      super(message)
      this.status = status
      this.code = code
      this.name = 'ApiError'
    }
  },
}))

vi.mock('../../lib/auth', () => ({
  isAuthenticated: mockIsAuthenticated,
  getAccessToken: () => 'test-token',
}))

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
  Toaster: () => null,
}))

const localStorageMock = (() => {
  let store: Record<string, string> = {}
  return {
    getItem: (key: string) => store[key] ?? null,
    setItem: (key: string, value: string) => { store[key] = value },
    removeItem: (key: string) => { delete store[key] },
    clear: () => { store = {} },
  }
})()

Object.defineProperty(global, 'localStorage', { value: localStorageMock, writable: true })

const mockJob = {
  id: 'job-1',
  title: 'Software Engineer',
  company: 'Acme Corp',
  url: 'https://acme.com/jobs/1',
  platform: 'greenhouse',
  status: 'pending',
  received_at: '2026-06-01T00:00:00Z',
}

const mockEvaluatedJob = { ...mockJob, status: 'evaluated', score: 4.2 }

const mockReport = {
  blocks_json: [
    { label: 'Role & Company Overview', content: 'Great role.' },
    { label: 'Match Analysis', content: 'Strong match.' },
  ],
  content_md: null,
}

const mockCV = {
  download_url: 'https://r2.example.com/cv.pdf',
  expires_at: '2026-06-02T00:00:00Z',
}

function mockGetFor({ job = mockJob, hasReport = false, hasCV = false } = {}) {
  mockApiGet.mockImplementation((path: string) => {
    if (path === '/api/jobs/job-1') return Promise.resolve(job)
    if (path === '/api/jobs/job-1/report') {
      return hasReport ? Promise.resolve(mockReport) : Promise.reject(new Error('404'))
    }
    if (path === '/api/jobs/job-1/cv') {
      return hasCV ? Promise.resolve(mockCV) : Promise.reject(new Error('404'))
    }
    return Promise.reject(new Error('unexpected path: ' + path))
  })
}

describe('Job detail page (app/jobs/[id]/page.tsx)', () => {
  beforeEach(() => {
    cleanup()
    localStorageMock.clear()
    localStorageMock.setItem('access_token', 'test-token')
    mockApiGet.mockReset()
    mockApiPost.mockReset()
    mockIsAuthenticated.mockReset()
    mockIsAuthenticated.mockReturnValue(true)
    mockRouter.push.mockReset()
    mockRouter.replace.mockReset()
  })

  it('calls apiGet for job/report/cv on mount and renders job header when no report/CV exists yet', async () => {
    mockGetFor({ job: mockJob, hasReport: false, hasCV: false })

    render(<JobDetailPage />)

    await waitFor(() => {
      expect(mockApiGet).toHaveBeenCalledWith('/api/jobs/job-1')
      expect(mockApiGet).toHaveBeenCalledWith('/api/jobs/job-1/report')
      expect(mockApiGet).toHaveBeenCalledWith('/api/jobs/job-1/cv')
    })

    await waitFor(() => {
      expect(screen.getByText('Software Engineer')).toBeTruthy()
      expect(screen.getByText('Acme Corp')).toBeTruthy()
    })

    expect(screen.getByText('greenhouse')).toBeTruthy()
    expect(screen.getByText('pending')).toBeTruthy()
    expect(screen.getByRole('button', { name: /^evaluate$/i })).toBeTruthy()
    expect(screen.queryByRole('button', { name: /generate cv/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /download cv/i })).toBeNull()
  })

  it('renders score badge and Re-evaluate/Generate CV buttons when job is evaluated', async () => {
    mockGetFor({ job: mockEvaluatedJob, hasReport: true, hasCV: false })

    render(<JobDetailPage />)

    await waitFor(() => {
      expect(screen.getByText('4.2')).toBeTruthy()
    })

    expect(screen.getByRole('button', { name: /re-evaluate/i })).toBeTruthy()
    expect(screen.getByRole('button', { name: /generate cv/i })).toBeTruthy()
  })

  it('renders Download CV button and Regenerate CV label when CV already exists', async () => {
    mockGetFor({ job: mockEvaluatedJob, hasReport: true, hasCV: true })

    render(<JobDetailPage />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /download cv/i })).toBeTruthy()
    })

    expect(screen.getByRole('button', { name: /regenerate cv/i })).toBeTruthy()
  })

  it('renders expandable report blocks and toggles content on click', async () => {
    mockGetFor({ job: mockEvaluatedJob, hasReport: true, hasCV: false })

    render(<JobDetailPage />)

    const blockButton = await screen.findByText('Role & Company Overview')
    expect(screen.queryByText('Great role.')).toBeNull()

    fireEvent.click(blockButton)

    await waitFor(() => {
      expect(screen.getByText('Great role.')).toBeTruthy()
    })
  })

  it('clicking Evaluate calls apiPost to the evaluate endpoint', async () => {
    mockGetFor({ job: mockJob, hasReport: false, hasCV: false })
    mockApiPost.mockResolvedValueOnce({})

    render(<JobDetailPage />)

    const evaluateButton = await screen.findByRole('button', { name: /^evaluate$/i })
    fireEvent.click(evaluateButton)

    await waitFor(() => {
      expect(mockApiPost).toHaveBeenCalledWith('/api/jobs/job-1/evaluate', {})
    })
  })

  it('shows CV-missing panel on 422 cv_missing', async () => {
    mockGetFor({ job: mockJob, hasReport: false, hasCV: false })
    mockApiPost.mockRejectedValueOnce(new ApiError(422, 'cv_missing', 'cv missing'))

    render(<JobDetailPage />)

    const evaluateButton = await screen.findByRole('button', { name: /^evaluate$/i })
    fireEvent.click(evaluateButton)

    await waitFor(() => {
      expect(screen.getByText('CV needed')).toBeTruthy()
      expect(screen.getByText(/upload your cv first/i)).toBeTruthy()
    })
  })

  it('shows JD-unavailable panel on 422 job_content_missing', async () => {
    mockGetFor({ job: mockJob, hasReport: false, hasCV: false })
    mockApiPost.mockRejectedValueOnce(new ApiError(422, 'job_content_missing', 'job content missing'))

    render(<JobDetailPage />)

    const evaluateButton = await screen.findByRole('button', { name: /^evaluate$/i })
    fireEvent.click(evaluateButton)

    await waitFor(() => {
      expect(screen.getByText('Job description unavailable')).toBeTruthy()
    })
  })

  it('shows generic toast on non-422 errors', async () => {
    mockGetFor({ job: mockJob, hasReport: false, hasCV: false })
    mockApiPost.mockRejectedValueOnce(new Error('network error'))

    render(<JobDetailPage />)

    const evaluateButton = await screen.findByRole('button', { name: /^evaluate$/i })
    fireEvent.click(evaluateButton)

    await waitFor(() => {
      expect(mockApiPost).toHaveBeenCalled()
    })
  })

  it('clicking Generate CV calls apiPost to the cv endpoint', async () => {
    mockGetFor({ job: mockEvaluatedJob, hasReport: true, hasCV: false })
    mockApiPost.mockResolvedValueOnce({})

    render(<JobDetailPage />)

    const generateButton = await screen.findByRole('button', { name: /generate cv/i })
    fireEvent.click(generateButton)

    await waitFor(() => {
      expect(mockApiPost).toHaveBeenCalledWith('/api/jobs/job-1/cv', {})
    })
  })

  it('redirects to / when job fails to load', async () => {
    mockApiGet.mockImplementation((path: string) => {
      if (path === '/api/jobs/job-1') return Promise.reject(new Error('not found'))
      return Promise.reject(new Error('404'))
    })

    render(<JobDetailPage />)

    await waitFor(() => {
      expect(mockRouter.replace).toHaveBeenCalledWith('/')
    })
  })

  it('redirects to /login when not authenticated', async () => {
    mockIsAuthenticated.mockReturnValue(false)

    render(<JobDetailPage />)

    await waitFor(() => {
      expect(mockRouter.replace).toHaveBeenCalledWith('/login')
    })
  })
})
