import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, waitFor, fireEvent, cleanup } from '@testing-library/react'
import React from 'react'
import { EmailIngestButtons } from '../../../features/email-ingest/EmailIngestButtons'

const { mockApiGet, mockApiPost } = vi.hoisted(() => ({
  mockApiGet: vi.fn(),
  mockApiPost: vi.fn(),
}))

vi.mock('../../../lib/api', () => ({
  apiGet: mockApiGet,
  apiPost: mockApiPost,
}))

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

describe('EmailIngestButtons', () => {
  beforeEach(() => {
    cleanup()
    mockApiGet.mockReset()
    mockApiPost.mockReset()
    // jsdom's window.location is not directly assignable; replace it wholesale.
    delete (window as unknown as { location?: unknown }).location
    window.location = { href: '' } as unknown as Location
  })

  it('"Connect Gmail" fetches the auth_url via the authenticated apiGet client, then navigates to it', async () => {
    // A plain window.location.href navigation to the API carries no
    // Authorization header — the endpoint is Bearer-authenticated, so it
    // MUST be reached through lib/api.ts's apiGet (token refresh included),
    // and only the returned auth_url is used for the actual browser nav.
    mockApiGet.mockResolvedValueOnce({ auth_url: 'https://accounts.google.com/o/oauth2/auth?scope=gmail.readonly&state=abc' })

    render(<EmailIngestButtons />)

    fireEvent.click(screen.getByRole('button', { name: /connect gmail/i }))

    await waitFor(() => {
      expect(mockApiGet).toHaveBeenCalledWith('/auth/google/gmail')
    })

    await waitFor(() => {
      expect(window.location.href).toBe('https://accounts.google.com/o/oauth2/auth?scope=gmail.readonly&state=abc')
    })
  })

  it('"Connect Gmail" surfaces an error toast and does not navigate when the fetch fails', async () => {
    mockApiGet.mockRejectedValueOnce(new Error('API error 401: unauthorized'))

    render(<EmailIngestButtons />)

    fireEvent.click(screen.getByRole('button', { name: /connect gmail/i }))

    const { toast } = await import('sonner')
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalled()
    })
    expect(window.location.href).toBe('')
  })

  it('"Sync email alerts" triggers POST /api/email-ingest and polls the run until completed', async () => {
    mockApiPost.mockResolvedValueOnce({ ingest_run_id: 'run-1' })
    mockApiGet
      .mockResolvedValueOnce({ id: 'run-1', status: 'running' })
      .mockResolvedValueOnce({ id: 'run-1', status: 'completed', new_jobs: 3 })

    render(<EmailIngestButtons pollIntervalMs={5} />)

    fireEvent.click(screen.getByRole('button', { name: /sync email alerts/i }))

    await waitFor(() => {
      expect(mockApiPost).toHaveBeenCalledWith('/api/email-ingest', {})
    })

    await waitFor(() => {
      expect(mockApiGet).toHaveBeenCalledWith('/api/email-ingest-runs/run-1')
    })

    await waitFor(() => {
      expect(screen.getByText(/completed/i)).toBeTruthy()
    })
  })

  it('shows partial/error status when the run finishes with those states', async () => {
    mockApiPost.mockResolvedValueOnce({ ingest_run_id: 'run-2' })
    mockApiGet.mockResolvedValueOnce({ id: 'run-2', status: 'error' })

    render(<EmailIngestButtons pollIntervalMs={5} />)

    fireEvent.click(screen.getByRole('button', { name: /sync email alerts/i }))

    await waitFor(() => {
      expect(screen.getByText(/error/i)).toBeTruthy()
    })

    // Button re-enables once the run reaches a terminal state.
    await waitFor(() => {
      const button = screen.getByRole('button', { name: /sync email alerts/i })
      expect(button).not.toBeDisabled()
    })
  })
})
