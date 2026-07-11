import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, waitFor, cleanup } from '@testing-library/react'
import React from 'react'
import AppLayout from '../../app/(app)/layout'

const { mockIsAuthenticated, mockRouter } = vi.hoisted(() => ({
  mockIsAuthenticated: vi.fn(() => true),
  mockRouter: { push: vi.fn(), replace: vi.fn() },
}))

vi.mock('next/navigation', () => ({
  useRouter: () => mockRouter,
  usePathname: () => '/',
}))

vi.mock('../../lib/auth', () => ({
  isAuthenticated: mockIsAuthenticated,
}))

beforeEach(() => {
  vi.clearAllMocks()
  cleanup()
})

describe('AppLayout auth guard', () => {
  it('redirects to /login when not authenticated', async () => {
    mockIsAuthenticated.mockReturnValue(false)
    render(<AppLayout><div>protected</div></AppLayout>)
    await waitFor(() => {
      expect(mockRouter.replace).toHaveBeenCalledWith('/login')
    })
  })

  it('does not redirect when authenticated', async () => {
    mockIsAuthenticated.mockReturnValue(true)
    render(<AppLayout><div>protected</div></AppLayout>)
    await waitFor(() => {
      expect(mockIsAuthenticated).toHaveBeenCalled()
    })
    expect(mockRouter.replace).not.toHaveBeenCalled()
  })
})
