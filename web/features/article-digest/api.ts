import { apiGet, apiPost, apiDelete } from '@/lib/api'

export interface ArticleDigest {
  id: string
  user_id: string
  title: string
  content_md: string
  created_at: string
}

export function listDigests(): Promise<{ digests: ArticleDigest[] }> {
  return apiGet<{ digests: ArticleDigest[] }>('/api/article-digests')
}

export function createDigest(title: string, content_md: string): Promise<ArticleDigest> {
  return apiPost<ArticleDigest>('/api/article-digests', { title, content_md })
}

export function deleteDigest(id: string): Promise<void> {
  return apiDelete(`/api/article-digests/${id}`)
}
