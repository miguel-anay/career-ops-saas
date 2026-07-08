import { getAccessToken, getRefreshToken, storeTokens, clearTokens } from './auth'

const API_URL = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8080'

type HttpMethod = 'GET' | 'POST' | 'PATCH' | 'DELETE' | 'PUT'

export class ApiError extends Error {
  status: number
  code: string | null

  constructor(status: number, code: string | null, message: string) {
    super(message)
    this.status = status
    this.code = code
    this.name = 'ApiError'
  }
}

async function refreshTokens(): Promise<boolean> {
  const refreshToken = getRefreshToken()
  if (!refreshToken) return false

  const response = await fetch(`${API_URL}/auth/refresh`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ refresh_token: refreshToken }),
  })

  if (!response.ok) {
    clearTokens()
    return false
  }

  const data = await response.json()
  storeTokens(data.access_token, data.refresh_token)
  return true
}

async function request<T>(
  method: HttpMethod,
  path: string,
  body?: unknown,
  isRetry = false
): Promise<T> {
  const token = getAccessToken()
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  }
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }

  const response = await fetch(`${API_URL}${path}`, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
    // API and web run on different origins (localhost:8080 vs :3000) — the
    // API's CORS middleware already allows credentials for the configured
    // origin, but the browser only stores/sends cross-origin cookies when
    // the request opts in. Needed for the gmail_oauth_state CSRF cookie set
    // by GET /auth/google/gmail to round-trip via this client.
    credentials: 'include',
  })

  if (response.status === 401 && !isRetry) {
    const refreshed = await refreshTokens()
    if (refreshed) {
      return request<T>(method, path, body, true)
    }
    // Redirect to login on second 401
    if (typeof window !== 'undefined') {
      window.location.href = '/login'
    }
    throw new Error('Unauthorized — redirecting to login')
  }

  if (!response.ok) {
    const errorText = await response.text()
    let code: string | null = null
    try {
      const parsed = JSON.parse(errorText)
      code = typeof parsed.code === 'string' ? parsed.code : null
    } catch {
      // best-effort — not all errors return JSON
    }
    throw new ApiError(response.status, code, errorText)
  }

  // Handle 204 No Content
  if (response.status === 204) {
    return undefined as T
  }

  return response.json() as Promise<T>
}

export function apiGet<T>(path: string): Promise<T> {
  return request<T>('GET', path)
}

export function apiPost<T>(path: string, body?: unknown): Promise<T> {
  return request<T>('POST', path, body)
}

export function apiPatch<T>(path: string, body: unknown): Promise<T> {
  return request<T>('PATCH', path, body)
}

export function apiDelete(path: string): Promise<void> {
  return request<void>('DELETE', path)
}
