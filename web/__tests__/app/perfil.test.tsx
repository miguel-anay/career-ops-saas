import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, waitFor, fireEvent, cleanup, within } from '@testing-library/react'
import React from 'react'
import PerfilPage from '../../app/(app)/perfil/page'

const { mockApiGet, mockApiPatch, mockApiPost, mockIsAuthenticated } = vi.hoisted(() => ({
  mockApiGet: vi.fn(),
  mockApiPatch: vi.fn(),
  mockApiPost: vi.fn(),
  mockIsAuthenticated: vi.fn(() => true),
}))

vi.mock('../../lib/api', () => ({
  apiGet: mockApiGet,
  apiPatch: mockApiPatch,
  apiPost: mockApiPost,
}))

vi.mock('../../lib/auth', () => ({
  isAuthenticated: mockIsAuthenticated,
  getAccessToken: () => 'test-token',
}))

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
  Toaster: () => null,
}))

const mockProfile = {
  cv_markdown: '# My CV\n\nExperience...',
  profile: {
    target_roles: { primary: ['Backend Engineer'] },
    salary_target: { min: 90000 },
    narrative: 'Some narrative',
    candidate: { full_name: 'Jane Doe' },
    deal_breakers: ['no on-call'],
    comp_targets: { equity: true },
  },
  edits: [
    { id: 'edit-1', field_path: 'narrative', old_value: 'old', new_value: 'Some narrative', source: 'manual', status: 'accepted', created_at: '2026-01-01T00:00:00Z' },
  ],
}

describe('Perfil page (app/perfil/page.tsx)', () => {
  beforeEach(() => {
    cleanup()
    mockApiGet.mockReset()
    mockApiPatch.mockReset()
    mockApiPost.mockReset()
    mockIsAuthenticated.mockReset()
    mockIsAuthenticated.mockReturnValue(true)
  })

  it('fetches the effective profile on mount and renders CV markdown + profile fields', async () => {
    mockApiGet.mockResolvedValueOnce(mockProfile)

    render(<PerfilPage />)

    await waitFor(() => {
      expect(mockApiGet).toHaveBeenCalledWith('/api/me/profile')
    })

    await waitFor(() => {
      expect(screen.getByText(/My CV/)).toBeTruthy()
    })

    expect(screen.getByLabelText('narrative')).toBeTruthy()
    expect(screen.getAllByText('narrative').length).toBeGreaterThan(0)
  })

  it('submitting the edit form calls apiPatch and refetches the profile', async () => {
    mockApiGet.mockResolvedValueOnce(mockProfile).mockResolvedValueOnce({
      ...mockProfile,
      profile: { ...mockProfile.profile, narrative: 'Updated narrative' },
    })
    mockApiPatch.mockResolvedValueOnce({ id: 'edit-2', field_path: 'narrative' })

    render(<PerfilPage />)

    const textarea = await screen.findByLabelText('narrative')
    fireEvent.change(textarea, { target: { value: '"Updated narrative"' } })

    const fieldContainer = textarea.closest('div') as HTMLElement
    const narrativeSaveButton = within(fieldContainer).getByRole('button', { name: /^save$/i })
    fireEvent.click(narrativeSaveButton)

    await waitFor(() => {
      expect(mockApiPatch).toHaveBeenCalledWith('/api/me/profile', { field_path: 'narrative', value: 'Updated narrative' })
    })

    await waitFor(() => {
      expect(mockApiGet).toHaveBeenCalledTimes(2)
    })
  })

  it('clicking Undo calls apiPost on the undo endpoint and refetches the profile', async () => {
    mockApiGet.mockResolvedValueOnce(mockProfile).mockResolvedValueOnce({
      ...mockProfile,
      edits: [{ ...mockProfile.edits[0], status: 'undone' }],
    })
    mockApiPost.mockResolvedValueOnce(undefined)

    render(<PerfilPage />)

    const undoButton = await screen.findByRole('button', { name: /^undo$/i })
    fireEvent.click(undoButton)

    await waitFor(() => {
      expect(mockApiPost).toHaveBeenCalledWith('/api/me/profile-edits/edit-1/undo')
    })

    await waitFor(() => {
      expect(mockApiGet).toHaveBeenCalledTimes(2)
    })
  })
})
