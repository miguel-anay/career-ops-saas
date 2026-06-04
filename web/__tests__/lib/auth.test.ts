import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'

// RED: These tests will fail until lib/auth.ts is implemented

describe('auth token management', () => {
  const localStorageMock = (() => {
    let store: Record<string, string> = {}
    return {
      getItem: (key: string) => store[key] ?? null,
      setItem: (key: string, value: string) => { store[key] = value },
      removeItem: (key: string) => { delete store[key] },
      clear: () => { store = {} },
    }
  })()

  beforeEach(() => {
    Object.defineProperty(global, 'localStorage', {
      value: localStorageMock,
      writable: true,
    })
    localStorageMock.clear()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('storeTokens saves access and refresh tokens to localStorage', async () => {
    const { storeTokens, getAccessToken, getRefreshToken } = await import('../../lib/auth')
    storeTokens('access123', 'refresh456')
    expect(getAccessToken()).toBe('access123')
    expect(getRefreshToken()).toBe('refresh456')
  })

  it('getAccessToken returns null when no token stored', async () => {
    const { getAccessToken } = await import('../../lib/auth')
    expect(getAccessToken()).toBeNull()
  })

  it('clearTokens removes both tokens from localStorage', async () => {
    const { storeTokens, clearTokens, getAccessToken, getRefreshToken } = await import('../../lib/auth')
    storeTokens('access123', 'refresh456')
    clearTokens()
    expect(getAccessToken()).toBeNull()
    expect(getRefreshToken()).toBeNull()
  })

  it('isAuthenticated returns false when no token stored', async () => {
    const { isAuthenticated } = await import('../../lib/auth')
    expect(isAuthenticated()).toBe(false)
  })

  it('isAuthenticated returns true when access token exists', async () => {
    const { storeTokens, isAuthenticated } = await import('../../lib/auth')
    storeTokens('access123', 'refresh456')
    expect(isAuthenticated()).toBe(true)
  })
})
