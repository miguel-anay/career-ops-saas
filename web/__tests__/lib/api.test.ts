import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'

// RED: These tests will fail until lib/api.ts is implemented

const localStorageMock = (() => {
  let store: Record<string, string> = {}
  return {
    getItem: (key: string) => store[key] ?? null,
    setItem: (key: string, value: string) => { store[key] = value },
    removeItem: (key: string) => { delete store[key] },
    clear: () => { store = {} },
  }
})()

Object.defineProperty(global, 'localStorage', {
  value: localStorageMock,
  writable: true,
})

describe('api fetch wrapper', () => {
  beforeEach(() => {
    localStorageMock.clear()
    localStorageMock.setItem('access_token', 'test-access-token')
    localStorageMock.setItem('refresh_token', 'test-refresh-token')
    vi.stubGlobal('fetch', vi.fn())
    // Stub window.location for redirect tests
    Object.defineProperty(global, 'window', {
      value: { location: { href: '' } },
      writable: true,
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('apiGet returns parsed JSON on success', async () => {
    const mockFetch = vi.fn().mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ jobs: [], total: 0, page: 1 }),
    })
    vi.stubGlobal('fetch', mockFetch)

    const { apiGet } = await import('../../lib/api')
    const result = await apiGet<{ jobs: []; total: number; page: number }>('/api/jobs')
    expect(result).toEqual({ jobs: [], total: 0, page: 1 })
    expect(mockFetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/jobs'),
      expect.objectContaining({
        headers: expect.objectContaining({
          Authorization: 'Bearer test-access-token',
        }),
      })
    )
  })

  it('apiGet on 401 refreshes token then retries successfully', async () => {
    const mockFetch = vi.fn()
      // First call: 401
      .mockResolvedValueOnce({ ok: false, status: 401, json: async () => ({}) })
      // Refresh call: success
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ access_token: 'new-access', refresh_token: 'new-refresh' }),
      })
      // Retry call: success
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ jobs: [], total: 0, page: 1 }),
      })
    vi.stubGlobal('fetch', mockFetch)

    const { apiGet } = await import('../../lib/api')
    const result = await apiGet<{ jobs: []; total: number; page: number }>('/api/jobs')
    expect(result).toEqual({ jobs: [], total: 0, page: 1 })
    expect(mockFetch).toHaveBeenCalledTimes(3)
  })

  it('apiGet on 401 after refresh redirects to /login', async () => {
    const mockFetch = vi.fn()
      // First call: 401
      .mockResolvedValueOnce({ ok: false, status: 401, json: async () => ({}) })
      // Refresh call: also 401
      .mockResolvedValueOnce({ ok: false, status: 401, json: async () => ({}) })
    vi.stubGlobal('fetch', mockFetch)

    const { apiGet } = await import('../../lib/api')
    await expect(apiGet('/api/jobs')).rejects.toThrow()
    expect(mockFetch).toHaveBeenCalledTimes(2)
  })
})
