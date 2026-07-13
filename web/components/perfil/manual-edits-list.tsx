'use client'

import { useState } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { apiPost } from '@/lib/api'

export type ProfileEdit = {
  id: string
  field_path: string
  old_value: unknown
  new_value: unknown
  source: string
  status: string
  created_at: string
}

type ManualEditsListProps = {
  edits: ProfileEdit[]
  onUndone: () => void
}

export function ManualEditsList({ edits, onUndone }: ManualEditsListProps) {
  const [undoingID, setUndoingID] = useState<string | null>(null)
  const activeEdits = edits.filter(e => e.status === 'accepted')

  const handleUndo = async (id: string) => {
    setUndoingID(id)
    try {
      await apiPost(`/api/me/profile-edits/${id}/undo`)
      toast.success('Edit undone')
      onUndone()
    } catch {
      toast.error('Failed to undo edit')
    } finally {
      setUndoingID(null)
    }
  }

  if (activeEdits.length === 0) {
    return <p className="text-sm text-muted-foreground">No manual edits yet.</p>
  }

  return (
    <ul className="space-y-2">
      {activeEdits.map(edit => (
        <li key={edit.id} className="flex items-center justify-between rounded-md border p-2 text-sm">
          <span>{edit.field_path}</span>
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={undoingID === edit.id}
            onClick={() => handleUndo(edit.id)}
          >
            Undo
          </Button>
        </li>
      ))}
    </ul>
  )
}
