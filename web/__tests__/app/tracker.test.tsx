import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import React from 'react'

// Mock next/navigation
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  redirect: vi.fn(),
}))

// Mock the api module
vi.mock('../../lib/api', () => ({
  apiGet: vi.fn(),
  apiPatch: vi.fn(),
}))

// Mock auth
vi.mock('../../lib/auth', () => ({
  isAuthenticated: vi.fn(() => true),
  getAccessToken: vi.fn(() => 'test-token'),
}))

// Mock sonner
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

const mockApplications = [
  {
    id: 'app-1',
    job_id: 'job-1',
    company: 'Acme Corp',
    role: 'Software Engineer',
    score: 4.2,
    status: 'Evaluated',
    notes: 'Good match',
    applied_at: '2026-06-01T00:00:00Z',
    pdf_path: null,
  },
]

describe('Tracker page (app/tracker/page.tsx)', () => {
  beforeEach(() => {
    localStorageMock.clear()
    localStorageMock.setItem('access_token', 'test-token')
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders applications table with data', async () => {
    const { apiGet } = await import('../../lib/api')
    vi.mocked(apiGet).mockResolvedValueOnce({ applications: mockApplications, total: 1 })

    const { default: TrackerPage } = await import('../../app/tracker/page')
    render(<TrackerPage />)

    await waitFor(() => {
      expect(screen.getByText('Acme Corp')).toBeTruthy()
      expect(screen.getByText('Software Engineer')).toBeTruthy()
    })
  })

  it('status select calls apiPatch on change', async () => {
    const { apiGet, apiPatch } = await import('../../lib/api')
    vi.mocked(apiGet).mockResolvedValueOnce({ applications: mockApplications, total: 1 })
    vi.mocked(apiPatch).mockResolvedValueOnce({ ...mockApplications[0], status: 'Applied' })

    const { default: TrackerPage } = await import('../../app/tracker/page')
    render(<TrackerPage />)

    await waitFor(() => {
      expect(screen.getByText('Acme Corp')).toBeTruthy()
    })

    // Find status select and trigger change
    const statusSelect = screen.getByDisplayValue('Evaluated')
    fireEvent.change(statusSelect, { target: { value: 'Applied' } })

    await waitFor(() => {
      expect(apiPatch).toHaveBeenCalledWith('/api/applications/app-1', { status: 'Applied' })
    })
  })
})
