'use client'

import { useState } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { apiPatch } from '@/lib/api'

// The 6 top-level keys the Go API allowlists for PATCH /api/me/profile
// (api/internal/profile/service.go's allowedFieldPaths). Kept in sync
// manually — small, fixed, unlikely-to-change set (D5).
const FIELD_PATHS = [
  'target_roles',
  'salary_target',
  'narrative',
  'candidate',
  'deal_breakers',
  'comp_targets',
] as const

type ProfileEditFormProps = {
  profile: Record<string, unknown>
  onSaved: () => void
}

export function ProfileEditForm({ profile, onSaved }: ProfileEditFormProps) {
  const [drafts, setDrafts] = useState<Record<string, string>>(() =>
    Object.fromEntries(FIELD_PATHS.map(fp => [fp, JSON.stringify(profile[fp] ?? null, null, 2)]))
  )
  const [savingField, setSavingField] = useState<string | null>(null)

  const handleSave = async (fieldPath: string) => {
    let value: unknown
    try {
      value = JSON.parse(drafts[fieldPath])
    } catch {
      toast.error(`${fieldPath}: invalid JSON`)
      return
    }

    setSavingField(fieldPath)
    try {
      await apiPatch('/api/me/profile', { field_path: fieldPath, value })
      toast.success(`${fieldPath} updated`)
      onSaved()
    } catch {
      toast.error(`Failed to update ${fieldPath}`)
    } finally {
      setSavingField(null)
    }
  }

  return (
    <div className="space-y-4">
      {FIELD_PATHS.map(fieldPath => (
        <div key={fieldPath} className="space-y-1">
          <label className="text-sm font-medium" htmlFor={`field-${fieldPath}`}>
            {fieldPath}
          </label>
          <Textarea
            id={`field-${fieldPath}`}
            value={drafts[fieldPath]}
            onChange={e => setDrafts(prev => ({ ...prev, [fieldPath]: e.target.value }))}
            rows={4}
          />
          <Button
            type="button"
            size="sm"
            disabled={savingField === fieldPath}
            onClick={() => handleSave(fieldPath)}
          >
            Save
          </Button>
        </div>
      ))}
    </div>
  )
}
