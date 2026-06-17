import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, waitFor, cleanup, fireEvent } from '@testing-library/react'
import React from 'react'
import TrackerPage from '../../app/tracker/page'

const { mockApiGet, mockApiPatch, mockRouter } = vi.hoisted(() => ({
  mockApiGet: vi.fn(),
  mockApiPatch: vi.fn(),
  // Stable reference: returning a new object each call makes useEffect re-run
  // on every render (router dep changes), causing a second loadApplications call
  // that detaches DOM elements before fireEvent can act on them.
  mockRouter: { push: vi.fn(), replace: vi.fn() },
}))

vi.mock('next/navigation', () => ({
  useRouter: () => mockRouter,
  redirect: vi.fn(),
}))

vi.mock('../../lib/api', () => ({
  apiGet: mockApiGet,
  apiPatch: mockApiPatch,
}))

vi.mock('../../lib/auth', () => ({
  isAuthenticated: () => true,
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
    cleanup()
    localStorageMock.clear()
    localStorageMock.setItem('access_token', 'test-token')
    mockApiGet.mockReset()
    mockApiPatch.mockReset()
  })

  it('renders applications table with data', async () => {
    mockApiGet.mockResolvedValueOnce({ applications: mockApplications, total: 1 })

    render(<TrackerPage />)

    await waitFor(() => {
      expect(screen.getByText('Acme Corp')).toBeTruthy()
      expect(screen.getByText('Software Engineer')).toBeTruthy()
    })
  })

  it('status select calls apiPatch on change', async () => {
    mockApiGet.mockResolvedValueOnce({ applications: mockApplications, total: 1 })
    mockApiPatch.mockResolvedValueOnce({ ...mockApplications[0], status: 'Applied' })

    const { container } = render(<TrackerPage />)

    let statusSelect: HTMLSelectElement | null = null
    await waitFor(() => {
      expect(screen.getByText('Acme Corp')).toBeTruthy()
      statusSelect = container.querySelector('select')
      expect(statusSelect).toBeTruthy()
    })

    fireEvent.change(statusSelect!, { target: { value: 'Applied' } })

    await waitFor(() => {
      expect(mockApiPatch).toHaveBeenCalledWith('/api/applications/app-1', { status: 'Applied' })
    })
  })
})
