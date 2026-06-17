import { test as base, Page } from '@playwright/test'

const MOCK_ACCESS_TOKEN = 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoiMDAwMDAwMDAtMDAwMC0wMDAwLTAwMDAtMDAwMDAwMDAwMDAxIiwicGxhbiI6ImZyZWUiLCJleHAiOjk5OTk5OTk5OTl9.stub'

export async function seedAuth(page: Page) {
  await page.addInitScript((token) => {
    localStorage.setItem('access_token', token)
    localStorage.setItem('refresh_token', token)
  }, MOCK_ACCESS_TOKEN)
}

export const mockJobs = [
  {
    id: 'job-1',
    title: 'Senior Software Engineer',
    company: 'Acme Corp',
    status: 'evaluated',
    score: 4.5,
    received_at: '2026-06-01T00:00:00Z',
    platform: 'greenhouse',
    url: 'https://greenhouse.io/jobs/1',
  },
  {
    id: 'job-2',
    title: 'Frontend Developer',
    company: 'Contoso',
    status: 'pending',
    received_at: '2026-06-02T00:00:00Z',
    platform: 'ashby',
  },
]

export const mockApplications = [
  {
    id: 'app-1',
    job_id: 'job-1',
    company: 'Acme Corp',
    role: 'Senior Software Engineer',
    score: 4.5,
    status: 'Evaluated',
    notes: 'Strong candidate',
    applied_at: '2026-06-01T00:00:00Z',
    pdf_path: null,
  },
]

export const mockCompanies = [
  {
    id: 'company-1',
    name: 'Acme Corp',
    careers_url: 'https://jobs.acme.com',
    provider_id: 'greenhouse',
    enabled: true,
  },
]

export const test = base
