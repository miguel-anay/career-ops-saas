import { apiGet, apiPost } from '@/lib/api'

export interface IngestRunResponse {
  run_id: string
}

export interface IngestionStatus {
  id: string
  status: 'pending' | 'processing' | 'completed' | 'failed'
  started_at: string
  finished_at: string | null
}

export function postIngest(rawCV: string): Promise<IngestRunResponse> {
  return apiPost<IngestRunResponse>('/api/cv/ingest', { raw_cv: rawCV })
}

export function getIngestion(runId: string): Promise<IngestionStatus> {
  return apiGet<IngestionStatus>(`/api/cv/ingest/${runId}`)
}
