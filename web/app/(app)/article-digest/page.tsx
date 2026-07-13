'use client'

import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { listDigests, createDigest, deleteDigest, type ArticleDigest } from '@/features/article-digest/api'

export default function ArticleDigestPage() {
  const [digests, setDigests] = useState<ArticleDigest[]>([])
  const [title, setTitle] = useState('')
  const [contentMd, setContentMd] = useState('')
  const [isSubmitting, setIsSubmitting] = useState(false)

  useEffect(() => {
    listDigests()
      .then(result => setDigests(result.digests))
      .catch(() => toast.error('Failed to load article digests'))
  }, [])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!title.trim() || !contentMd.trim()) return

    setIsSubmitting(true)
    try {
      const created = await createDigest(title.trim(), contentMd.trim())
      setDigests(prev => [created, ...prev])
      setTitle('')
      setContentMd('')
    } catch {
      toast.error('Failed to create digest entry')
    } finally {
      setIsSubmitting(false)
    }
  }

  const handleDelete = async (id: string) => {
    try {
      await deleteDigest(id)
      setDigests(prev => prev.filter(d => d.id !== id))
    } catch {
      toast.error('Failed to delete digest entry')
    }
  }

  return (
    <div className="container mx-auto p-6 space-y-6 max-w-2xl">
      <h1 className="text-2xl font-bold">Article Digest</h1>

      <form onSubmit={handleSubmit} className="space-y-4">
        <Input
          placeholder="Title"
          value={title}
          onChange={e => setTitle(e.target.value)}
        />
        <Textarea
          placeholder="Markdown body — hero metrics, architecture, key decisions, proof points…"
          value={contentMd}
          onChange={e => setContentMd(e.target.value)}
          rows={10}
        />
        <Button type="submit" disabled={isSubmitting || !title.trim() || !contentMd.trim()}>
          {isSubmitting ? 'Adding…' : 'Add'}
        </Button>
      </form>

      <ul className="space-y-3">
        {digests.map(digest => (
          <li key={digest.id} className="rounded border p-4 space-y-2">
            <div className="flex items-center justify-between">
              <h2 className="font-medium">{digest.title}</h2>
              <Button variant="outline" size="sm" onClick={() => handleDelete(digest.id)}>
                Delete
              </Button>
            </div>
            <pre className="text-xs whitespace-pre-wrap break-words text-muted-foreground">
              {digest.content_md}
            </pre>
          </li>
        ))}
      </ul>
    </div>
  )
}
