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
  apiPost: vi.fn(),
}))

// Mock the auth module
vi.mock('../../lib/auth', () => ({
  isAuthenticated: vi.fn(),
  getAccessToken: vi.fn(() => 'test-token'),
}))

// Mock useScanProgress hook
vi.mock('../../hooks/useScanProgress', () => ({
  useScanProgress: () => ({
    events: [],
    status: 'idle',
    isConnected: false,
    error: null,
    connect: vi.fn(),
    reset: vi.fn(),
  }),
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

describe('Dashboard page (app/page.tsx)', () => {
  beforeEach(() => {
    localStorageMock.clear()
    localStorageMock.setItem('access_token', 'test-token')
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders job list when authenticated', async () => {
    const { isAuthenticated } = await import('../../lib/auth')
    const { apiGet } = await import('../../lib/api')
    vi.mocked(isAuthenticated).mockReturnValue(true)
    vi.mocked(apiGet).mockResolvedValueOnce({
      jobs: [
        { id: '1', title: 'Software Engineer', company: 'Acme', status: 'pending', received_at: '2026-06-01T00:00:00Z', platform: 'greenhouse' },
      ],
      total: 1,
      page: 1,
    })

    const { default: DashboardPage } = await import('../../app/page')
    render(<DashboardPage />)

    await waitFor(() => {
      expect(screen.getByText('Software Engineer')).toBeTruthy()
    })
  })

  it('"Add Job URL" form calls apiPost on submit', async () => {
    const { isAuthenticated } = await import('../../lib/auth')
    const { apiGet, apiPost } = await import('../../lib/api')
    vi.mocked(isAuthenticated).mockReturnValue(true)
    vi.mocked(apiGet).mockResolvedValue({ jobs: [], total: 0, page: 1 })
    vi.mocked(apiPost).mockResolvedValueOnce({ id: '2', url: 'http://example.com', status: 'pending', platform: 'ashby' })

    const { default: DashboardPage } = await import('../../app/page')
    render(<DashboardPage />)

    const input = await screen.findByPlaceholderText(/job url/i)
    const button = screen.getByRole('button', { name: /add/i })

    fireEvent.change(input, { target: { value: 'http://example.com/job' } })
    fireEvent.click(button)

    await waitFor(() => {
      expect(apiPost).toHaveBeenCalledWith('/api/jobs', { url: 'http://example.com/job' })
    })
  })

  it('redirects to /login when not authenticated', async () => {
    const { isAuthenticated } = await import('../../lib/auth')
    const { redirect } = await import('next/navigation')
    vi.mocked(isAuthenticated).mockReturnValue(false)

    const { default: DashboardPage } = await import('../../app/page')
    render(<DashboardPage />)

    await waitFor(() => {
      expect(redirect).toHaveBeenCalledWith('/login')
    })
  })
})
