export interface Job {
  id: string
  title: string
  company: string
  status: string
  score?: number
  received_at: string
  platform: string
  url?: string
}

export interface JobsResponse {
  jobs: Job[]
  total: number
  page: number
}
