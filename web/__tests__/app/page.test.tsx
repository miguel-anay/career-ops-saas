import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, waitFor, fireEvent, cleanup } from '@testing-library/react'
import React from 'react'
import DashboardPage from '../../app/(app)/jobs/page'

const { mockApiGet, mockApiPost, mockIsAuthenticated, mockRouter } = vi.hoisted(() => ({
  mockApiGet: vi.fn(),
  mockApiPost: vi.fn(),
  mockIsAuthenticated: vi.fn(() => true),
  mockRouter: { push: vi.fn(), replace: vi.fn() },
}))

vi.mock('next/navigation', () => ({
  useRouter: () => mockRouter,
  redirect: vi.fn(),
}))

vi.mock('../../lib/api', () => ({
  apiGet: mockApiGet,
  apiPost: mockApiPost,
}))

vi.mock('../../lib/auth', () => ({
  isAuthenticated: mockIsAuthenticated,
  getAccessToken: () => 'test-token',
}))

vi.mock('../../features/jobs/hooks', () => ({
  useScanProgress: () => ({
    events: [],
    status: 'idle',
    isConnected: false,
    error: null,
    connect: vi.fn(),
    reset: vi.fn(),
  }),
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

describe('Dashboard page (app/page.tsx)', () => {
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

  it('renders job list when authenticated', async () => {
    mockApiGet.mockResolvedValueOnce({
      jobs: [
        { id: '1', title: 'Software Engineer', company: 'Acme', status: 'pending', received_at: '2026-06-01T00:00:00Z', platform: 'greenhouse' },
      ],
      total: 1,
      page: 1,
    })

    render(<DashboardPage />)

    await waitFor(() => {
      expect(screen.getByText('Software Engineer')).toBeTruthy()
    })
  })

  it('"Add Job URL" form calls apiPost on submit', async () => {
    mockApiGet.mockResolvedValue({ jobs: [], total: 0, page: 1 })
    mockApiPost.mockResolvedValueOnce({ id: '2', url: 'http://example.com', status: 'pending', platform: 'ashby' })

    render(<DashboardPage />)

    const input = await screen.findByPlaceholderText(/job url/i)
    const button = screen.getByRole('button', { name: /^add$/i })

    fireEvent.change(input, { target: { value: 'http://example.com/job' } })
    fireEvent.click(button)

    await waitFor(() => {
      expect(mockApiPost).toHaveBeenCalledWith('/api/jobs', { url: 'http://example.com/job' })
    })
  })

})
