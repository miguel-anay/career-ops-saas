import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, waitFor, cleanup, fireEvent, act } from '@testing-library/react'
import React from 'react'
import IngestCVPage from '../../app/(app)/cv/ingest/page'

const { mockPostIngest, mockGetIngestion, mockRouter, mockConnect, mockReset, mockUseJobProgress } = vi.hoisted(() => ({
  mockPostIngest: vi.fn(),
  mockGetIngestion: vi.fn(),
  mockRouter: { push: vi.fn(), replace: vi.fn() },
  mockConnect: vi.fn(),
  mockReset: vi.fn(),
  mockUseJobProgress: vi.fn(),
}))

vi.mock('next/navigation', () => ({
  useRouter: () => mockRouter,
  redirect: vi.fn(),
}))

vi.mock('../../features/cv/api', () => ({
  postIngest: mockPostIngest,
  getIngestion: mockGetIngestion,
}))

vi.mock('../../lib/auth', () => ({
  isAuthenticated: () => true,
  getAccessToken: () => 'test-token',
}))

vi.mock('../../features/cv/hooks', () => ({
  useJobProgress: mockUseJobProgress,
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

const RUN_ID = '11111111-1111-1111-1111-111111111111'

function setHookState(overrides: Partial<{
  status: string
  payload: Record<string, unknown> | null
  isConnected: boolean
  error: string | null
}> = {}) {
  mockUseJobProgress.mockReturnValue({
    status: 'idle',
    payload: null,
    isConnected: false,
    error: null,
    connect: mockConnect,
    reset: mockReset,
    ...overrides,
  })
}

describe('Ingest CV page (app/cv/ingest/page.tsx)', () => {
  beforeEach(() => {
    cleanup()
    vi.useRealTimers()
    localStorageMock.clear()
    localStorageMock.setItem('access_token', 'test-token')
    mockPostIngest.mockReset()
    mockGetIngestion.mockReset()
    mockConnect.mockReset()
    mockReset.mockReset()
    setHookState()
  })

  it('submitting the textarea calls postIngest with the raw CV and connects the hook with the returned run_id', async () => {
    mockPostIngest.mockResolvedValueOnce({ run_id: RUN_ID })

    render(<IngestCVPage />)

    const textarea = screen.getByPlaceholderText(/paste your cv/i)
    fireEvent.change(textarea, { target: { value: 'My CV content' } })

    const submitButton = screen.getByRole('button', { name: /submit/i })
    fireEvent.click(submitButton)

    await waitFor(() => {
      expect(mockPostIngest).toHaveBeenCalledWith('My CV content')
    })

    await waitFor(() => {
      expect(mockConnect).toHaveBeenCalledWith(RUN_ID)
    })
  })

  it('does not submit an empty or whitespace-only CV', async () => {
    render(<IngestCVPage />)

    const submitButton = screen.getByRole('button', { name: /submit/i })
    expect(submitButton).toBeDisabled()

    const textarea = screen.getByPlaceholderText(/paste your cv/i)
    fireEvent.change(textarea, { target: { value: '   ' } })

    expect(screen.getByRole('button', { name: /submit/i })).toBeDisabled()
    expect(mockPostIngest).not.toHaveBeenCalled()
  })

  it('renders live status from useJobProgress while working', async () => {
    setHookState({ status: 'working' })

    render(<IngestCVPage />)

    expect(screen.getByText(/processing/i)).toBeTruthy()
  })

  it('renders the completed payload when useJobProgress reaches completed', async () => {
    setHookState({
      status: 'completed',
      payload: { run_id: RUN_ID, status: 'completed', profile_json: { candidate: { full_name: 'Ada Lovelace' } } },
    })

    render(<IngestCVPage />)

    expect(screen.getByText('Completed')).toBeTruthy()
  })

  it('renders an error state when useJobProgress reaches error', async () => {
    setHookState({ status: 'error', payload: { error: 'anthropic_error' } })

    render(<IngestCVPage />)

    expect(screen.getByText(/failed/i)).toBeTruthy()
  })

  it('falls back to polling GET /api/cv/ingest/:id when the WS drops (status not connected, not idle)', async () => {
    vi.useFakeTimers()
    setHookState({ status: 'working', isConnected: false })
    mockGetIngestion.mockResolvedValue({ id: RUN_ID, status: 'completed', started_at: '2026-01-01T00:00:00Z', finished_at: '2026-01-01T00:01:00Z' })

    render(<IngestCVPage />)

    // Submit first so the page has a run_id to poll
    const textarea = screen.getByPlaceholderText(/paste your cv/i)
    fireEvent.change(textarea, { target: { value: 'My CV content' } })

    mockPostIngest.mockResolvedValueOnce({ run_id: RUN_ID })
    const submitButton = screen.getByRole('button', { name: /submit/i })

    await act(async () => {
      fireEvent.click(submitButton)
      await Promise.resolve()
    })

    await act(async () => {
      await vi.advanceTimersByTimeAsync(4000)
    })

    expect(mockGetIngestion).toHaveBeenCalledWith(RUN_ID)
    vi.useRealTimers()
  })
})
