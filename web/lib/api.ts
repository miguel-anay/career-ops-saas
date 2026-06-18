import { getAccessToken, getRefreshToken, storeTokens, clearTokens } from './auth'

const API_URL = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8080'

type HttpMethod = 'GET' | 'POST' | 'PATCH' | 'DELETE' | 'PUT'

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
    throw new Error(`API error ${response.status}: ${errorText}`)
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

export interface IngestRunResponse {
  run_id: string
}

export interface IngestionStatus {
  id: string
  status: 'pending' | 'processing' | 'completed' | 'failed'
  started_at: string
  finished_at: string | null
}

export function postIngest(rawCV: string): Promise<IngestRunResponse> {
  return apiPost<IngestRunResponse>('/api/cv/ingest', { raw_cv: rawCV })
}

export function getIngestion(runId: string): Promise<IngestionStatus> {
  return apiGet<IngestionStatus>(`/api/cv/ingest/${runId}`)
}
