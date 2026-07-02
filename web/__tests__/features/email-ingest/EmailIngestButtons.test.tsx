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

  it('"Connect Gmail" navigates to /auth/google/gmail', () => {
    render(<EmailIngestButtons />)

    fireEvent.click(screen.getByRole('button', { name: /connect gmail/i }))

    expect(window.location.href).toContain('/auth/google/gmail')
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
