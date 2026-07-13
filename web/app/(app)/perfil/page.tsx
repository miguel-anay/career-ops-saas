'use client'

import { useCallback, useEffect, useState } from 'react'
import { toast } from 'sonner'
import { apiGet } from '@/lib/api'
import { isAuthenticated } from '@/lib/auth'
import { CvMarkdownView } from '@/components/perfil/cv-markdown-view'
import { ProfileEditForm } from '@/components/perfil/profile-edit-form'
import { ManualEditsList, type ProfileEdit } from '@/components/perfil/manual-edits-list'

type EffectiveProfileResponse = {
  cv_markdown: string
  profile: Record<string, unknown>
  edits: ProfileEdit[]
}

export default function PerfilPage() {
  const [data, setData] = useState<EffectiveProfileResponse | null>(null)
  const [isLoading, setIsLoading] = useState(true)

  const loadProfile = useCallback(async () => {
    setIsLoading(true)
    try {
      const result = await apiGet<EffectiveProfileResponse>('/api/me/profile')
      setData(result)
    } catch {
      toast.error('Failed to load profile')
    } finally {
      setIsLoading(false)
    }
  }, [])

  useEffect(() => {
    if (!isAuthenticated()) return
    loadProfile()
  }, [loadProfile])

  if (isLoading) {
    return <p className="p-4 text-sm text-muted-foreground">Loading...</p>
  }

  if (!data) {
    return <p className="p-4 text-sm text-muted-foreground">Failed to load profile.</p>
  }

  return (
    <div className="space-y-6 p-4">
      <h1 className="text-lg font-semibold">Perfil</h1>
      <CvMarkdownView cvMarkdown={data.cv_markdown} />
      <ProfileEditForm profile={data.profile} onSaved={loadProfile} />
      <ManualEditsList edits={data.edits} onUndone={loadProfile} />
    </div>
  )
}
