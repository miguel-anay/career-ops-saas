import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, waitFor, fireEvent, cleanup } from '@testing-library/react'
import React from 'react'
import ArticleDigestPage from '../../app/(app)/article-digest/page'

const { mockListDigests, mockCreateDigest, mockDeleteDigest } = vi.hoisted(() => ({
  mockListDigests: vi.fn(),
  mockCreateDigest: vi.fn(),
  mockDeleteDigest: vi.fn(),
}))

vi.mock('../../features/article-digest/api', () => ({
  listDigests: mockListDigests,
  createDigest: mockCreateDigest,
  deleteDigest: mockDeleteDigest,
}))

vi.mock('../../lib/auth', () => ({
  isAuthenticated: () => true,
  getAccessToken: () => 'test-token',
}))

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
  Toaster: () => null,
}))

const mockDigest = {
  id: 'd1',
  user_id: 'u1',
  title: 'Fraud Detection Pipeline',
  content_md: '**Hero metrics:** cut false positives 40%',
  created_at: '2026-01-01T00:00:00Z',
}

describe('Article Digest page (app/article-digest/page.tsx)', () => {
  beforeEach(() => {
    cleanup()
    mockListDigests.mockReset()
    mockCreateDigest.mockReset()
    mockDeleteDigest.mockReset()
  })

  it('renders an empty state with no digests and no stray list markup', async () => {
    mockListDigests.mockResolvedValueOnce({ digests: [] })

    render(<ArticleDigestPage />)

    await waitFor(() => {
      expect(mockListDigests).toHaveBeenCalled()
    })

    expect(screen.queryByText('Fraud Detection Pipeline')).toBeNull()
  })

  it('submitting the create form adds the returned entry to the rendered list', async () => {
    mockListDigests.mockResolvedValueOnce({ digests: [] })
    mockCreateDigest.mockResolvedValueOnce(mockDigest)

    render(<ArticleDigestPage />)

    await waitFor(() => {
      expect(mockListDigests).toHaveBeenCalled()
    })

    const titleInput = screen.getByPlaceholderText(/title/i)
    const bodyTextarea = screen.getByPlaceholderText(/markdown/i)
    fireEvent.change(titleInput, { target: { value: mockDigest.title } })
    fireEvent.change(bodyTextarea, { target: { value: mockDigest.content_md } })

    const submitButton = screen.getByRole('button', { name: /add/i })
    fireEvent.click(submitButton)

    await waitFor(() => {
      expect(mockCreateDigest).toHaveBeenCalledWith(mockDigest.title, mockDigest.content_md)
    })

    await waitFor(() => {
      expect(screen.getByText('Fraud Detection Pipeline')).toBeTruthy()
    })
  })

  it('clicking Delete on an entry removes it from the rendered list', async () => {
    mockListDigests.mockResolvedValueOnce({ digests: [mockDigest] })
    mockDeleteDigest.mockResolvedValueOnce(undefined)

    render(<ArticleDigestPage />)

    await waitFor(() => {
      expect(screen.getByText('Fraud Detection Pipeline')).toBeTruthy()
    })

    const deleteButton = screen.getByRole('button', { name: /delete/i })
    fireEvent.click(deleteButton)

    await waitFor(() => {
      expect(mockDeleteDigest).toHaveBeenCalledWith('d1')
    })

    await waitFor(() => {
      expect(screen.queryByText('Fraud Detection Pipeline')).toBeNull()
    })
  })
})
