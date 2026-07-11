import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, waitFor, fireEvent, cleanup } from '@testing-library/react'
import React from 'react'
import CompaniesPage from '../../app/(app)/companies/page'

const { mockApiGet, mockApiPost, mockApiDelete, mockIsAuthenticated, mockRouter } = vi.hoisted(() => ({
  mockApiGet: vi.fn(),
  mockApiPost: vi.fn(),
  mockApiDelete: vi.fn(),
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
  apiDelete: mockApiDelete,
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

const mockCompanies = [
  { id: 'c1', name: 'Acme Corp', careers_url: 'https://acme.com/careers', provider_id: 'greenhouse', enabled: true },
]

const mockCatalog = [
  { id: 'cat-1', name: 'Acme Corp', careers_url: 'https://acme.com/careers', provider_id: 'greenhouse', ats_api_url: 'https://api.greenhouse.io/acme' },
  { id: 'cat-2', name: 'Globex Inc', careers_url: 'https://globex.com/careers', provider_id: 'ashby', ats_api_url: 'https://api.ashby.io/globex' },
]

describe('Companies page (app/companies/page.tsx)', () => {
  beforeEach(() => {
    cleanup()
    localStorageMock.clear()
    localStorageMock.setItem('access_token', 'test-token')
    mockApiGet.mockReset()
    mockApiPost.mockReset()
    mockApiDelete.mockReset()
    mockIsAuthenticated.mockReset()
    mockIsAuthenticated.mockReturnValue(true)
    mockRouter.push.mockReset()
    mockRouter.replace.mockReset()
  })

  it('calls apiGet for companies and catalog on mount, and renders watched companies table', async () => {
    mockApiGet.mockImplementation((path: string) => {
      if (path === '/api/companies') return Promise.resolve({ companies: mockCompanies })
      if (path === '/api/companies/catalog') return Promise.resolve({ catalog: mockCatalog })
      return Promise.reject(new Error('unexpected path'))
    })

    render(<CompaniesPage />)

    await waitFor(() => {
      expect(mockApiGet).toHaveBeenCalledWith('/api/companies')
      expect(mockApiGet).toHaveBeenCalledWith('/api/companies/catalog')
    })

    await waitFor(() => {
      expect(screen.getByText('Acme Corp')).toBeTruthy()
      expect(screen.getByText('https://acme.com/careers')).toBeTruthy()
    })

    expect(screen.getByText('Watched Companies')).toBeTruthy()
    expect(screen.getByPlaceholderText(/search companies to watch/i)).toBeTruthy()
  })

  it('typing in the catalog search filters results by name', async () => {
    mockApiGet.mockImplementation((path: string) => {
      if (path === '/api/companies') return Promise.resolve({ companies: [] })
      if (path === '/api/companies/catalog') return Promise.resolve({ catalog: mockCatalog })
      return Promise.reject(new Error('unexpected path'))
    })

    render(<CompaniesPage />)

    const input = await screen.findByPlaceholderText(/search companies to watch/i)
    fireEvent.focus(input)

    await waitFor(() => {
      expect(screen.getByText('Globex Inc')).toBeTruthy()
    })

    fireEvent.change(input, { target: { value: 'globex' } })

    await waitFor(() => {
      expect(screen.getByText('Globex Inc')).toBeTruthy()
      expect(screen.queryByText('Acme Corp')).toBeNull()
    })
  })

  it('selecting a catalog entry calls apiPost with catalog_id', async () => {
    mockApiGet.mockImplementation((path: string) => {
      if (path === '/api/companies') return Promise.resolve({ companies: [] })
      if (path === '/api/companies/catalog') return Promise.resolve({ catalog: mockCatalog })
      return Promise.reject(new Error('unexpected path'))
    })
    mockApiPost.mockResolvedValueOnce({})

    render(<CompaniesPage />)

    const input = await screen.findByPlaceholderText(/search companies to watch/i)
    fireEvent.focus(input)

    const option = await screen.findByText('Globex Inc')
    fireEvent.click(option)

    await waitFor(() => {
      expect(mockApiPost).toHaveBeenCalledWith('/api/companies', { catalog_id: 'cat-2' })
    })
  })

  it('clicking Remove then confirming calls apiDelete with company id', async () => {
    mockApiGet.mockImplementation((path: string) => {
      if (path === '/api/companies') return Promise.resolve({ companies: mockCompanies })
      if (path === '/api/companies/catalog') return Promise.resolve({ catalog: mockCatalog })
      return Promise.reject(new Error('unexpected path'))
    })
    mockApiDelete.mockResolvedValueOnce(undefined)

    render(<CompaniesPage />)

    await waitFor(() => {
      expect(screen.getByText('Acme Corp')).toBeTruthy()
    })

    const removeButton = screen.getByRole('button', { name: /^remove$/i })
    fireEvent.click(removeButton)

    // The dialog opens and marks the rest of the page inert (aria-hidden),
    // so only the dialog footer's "Remove" button remains in the accessibility tree.
    const confirmButton = await screen.findByText('Remove Acme Corp?')
    expect(confirmButton).toBeTruthy()
    const confirmRemoveButton = await screen.findByRole('button', { name: /^remove$/i })
    fireEvent.click(confirmRemoveButton)

    await waitFor(() => {
      expect(mockApiDelete).toHaveBeenCalledWith('/api/companies/c1')
    })
  })

})
