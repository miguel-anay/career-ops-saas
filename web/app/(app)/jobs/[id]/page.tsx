'use client'

import { useEffect, useState } from 'react'
import { useParams, useRouter } from 'next/navigation'
import Link from 'next/link'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { apiGet, apiPost, ApiError } from '@/lib/api'
import { isAuthenticated } from '@/lib/auth'

interface Job {
  id: string
  title: string
  company: string
  url: string
  platform: string
  status: string
  score?: number
  received_at: string
}

interface ReportBlock {
  label: string
  content: string
}

interface Report {
  blocks_json: ReportBlock[] | null
  content_md: string | null
}

interface CVResponse {
  download_url: string
  expires_at: string
}

const BLOCK_LABELS: Record<string, string> = {
  A: 'Role & Company Overview',
  B: 'Match Analysis',
  C: 'Compensation & Benefits',
  D: 'Culture & Alignment',
  E: 'Red Flags',
  F: 'Application Strategy',
  G: 'Posting Legitimacy',
}

function ScoreBadge({ score }: { score: number }) {
  const color = score >= 4 ? 'text-green-600' : score >= 3 ? 'text-yellow-600' : 'text-red-600'
  return (
    <span className={`text-3xl font-bold ${color}`}>
      {score.toFixed(1)}
      <span className="text-base font-normal text-muted-foreground">/5</span>
    </span>
  )
}

export default function JobDetailPage() {
  const params = useParams()
  const router = useRouter()
  const jobId = params.id as string

  const [job, setJob] = useState<Job | null>(null)
  const [report, setReport] = useState<Report | null>(null)
  const [cv, setCV] = useState<CVResponse | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [isEvaluating, setIsEvaluating] = useState(false)
  const [isGeneratingCV, setIsGeneratingCV] = useState(false)
  const [expandedBlocks, setExpandedBlocks] = useState<Set<string>>(new Set())
  const [evalError, setEvalError] = useState<'cv_missing' | 'job_content_missing' | null>(null)

  useEffect(() => {
    if (!isAuthenticated()) {
      router.replace('/login')
      return
    }
    async function loadData() {
      setIsLoading(true)
      try {
        const jobData = await apiGet<Job>(`/api/jobs/${jobId}`)
        setJob(jobData)

        // Try to load report (404 is OK)
        try {
          const reportData = await apiGet<Report>(`/api/jobs/${jobId}/report`)
          setReport(reportData)
        } catch {
          // No report yet
        }

        // Try to load CV
        try {
          const cvData = await apiGet<CVResponse>(`/api/jobs/${jobId}/cv`)
          setCV(cvData)
        } catch {
          // No CV yet
        }
      } catch {
        toast.error('Failed to load job')
        router.replace('/')
      } finally {
        setIsLoading(false)
      }
    }
    loadData()
  }, [jobId, router])

  const handleEvaluate = async () => {
    setEvalError(null)
    setIsEvaluating(true)
    try {
      await apiPost(`/api/jobs/${jobId}/evaluate`, {})
      toast.success('Evaluation queued — this may take a minute')
    } catch (err) {
      if (err instanceof ApiError && err.status === 422) {
        setEvalError(err.code as 'cv_missing' | 'job_content_missing')
      } else {
        toast.error('Failed to queue evaluation')
      }
    } finally {
      setIsEvaluating(false)
    }
  }

  const handleGenerateCV = async () => {
    setIsGeneratingCV(true)
    try {
      await apiPost(`/api/jobs/${jobId}/cv`, {})
      toast.success('CV generation queued')
    } catch {
      toast.error('Failed to queue CV generation')
    } finally {
      setIsGeneratingCV(false)
    }
  }

  const toggleBlock = (key: string) => {
    setExpandedBlocks(prev => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  if (isLoading) {
    return (
      <div className="container mx-auto p-6">
        <p className="text-muted-foreground">Loading…</p>
      </div>
    )
  }

  if (!job) return null

  return (
    <div className="container mx-auto p-6 space-y-6 max-w-4xl">
      {/* Back */}
      <Link href="/jobs" className="text-sm text-muted-foreground hover:underline">
        ← Back to pipeline
      </Link>

      {/* Header */}
      <div className="flex items-start justify-between gap-4">
        <div className="space-y-1">
          <h1 className="text-2xl font-bold">{job.title || '(untitled)'}</h1>
          <p className="text-lg text-muted-foreground">{job.company}</p>
          {job.url && (
            <a
              href={job.url}
              target="_blank"
              rel="noopener noreferrer"
              className="text-sm text-blue-600 hover:underline"
            >
              {job.url}
            </a>
          )}
          <div className="flex gap-2 items-center">
            <Badge variant="outline">{job.platform}</Badge>
            <Badge variant="secondary">{job.status}</Badge>
          </div>
        </div>
        {job.score !== undefined && job.score !== null && (
          <div className="text-center">
            <ScoreBadge score={job.score} />
            <p className="text-xs text-muted-foreground mt-1">score</p>
          </div>
        )}
      </div>

      {/* Action buttons */}
      <div className="flex gap-3">
        <Button
          onClick={handleEvaluate}
          disabled={isEvaluating}
          variant={job.status === 'evaluated' ? 'outline' : 'default'}
        >
          {isEvaluating ? 'Queueing…' : job.status === 'evaluated' ? 'Re-evaluate' : 'Evaluate'}
        </Button>
        {job.status === 'evaluated' && (
          <Button
            onClick={handleGenerateCV}
            disabled={isGeneratingCV}
            variant="outline"
          >
            {isGeneratingCV ? 'Queueing…' : cv ? 'Regenerate CV' : 'Generate CV'}
          </Button>
        )}
        {cv && (
          <a href={cv.download_url} target="_blank" rel="noopener noreferrer">
            <Button variant="outline">Download CV</Button>
          </a>
        )}
      </div>

      {/* 422 evaluation error panels */}
      {evalError === 'cv_missing' && (
        <div className="border border-yellow-300 bg-yellow-50 rounded-lg p-4 text-sm text-yellow-800">
          <p className="font-medium mb-1">CV needed</p>
          <p>
            Upload your CV first before evaluating jobs.{' '}
            <Link href="/cv/ingest" className="underline font-medium">Add your CV</Link>
          </p>
        </div>
      )}
      {evalError === 'job_content_missing' && (
        <div className="border border-yellow-300 bg-yellow-50 rounded-lg p-4 text-sm text-yellow-800">
          <p className="font-medium mb-1">Job description unavailable</p>
          <p>
            This job has no readable description yet. Jobs ingested from ATS providers are
            automatically populated; for manual entries, edit the job to add a description.
          </p>
        </div>
      )}

      {/* Report blocks */}
      {report && (
        <div className="space-y-3">
          <h2 className="text-lg font-semibold">Evaluation Report</h2>
          {report.blocks_json && report.blocks_json.length > 0 ? (
            report.blocks_json.map((block, i) => {
              const key = String(i)
              const label = block.label || BLOCK_LABELS[String.fromCharCode(65 + i)] || `Block ${i + 1}`
              const isExpanded = expandedBlocks.has(key)
              return (
                <div key={key} className="border rounded-lg overflow-hidden">
                  <button
                    onClick={() => toggleBlock(key)}
                    className="w-full flex items-center justify-between p-4 text-left hover:bg-muted/50"
                  >
                    <span className="font-medium">{label}</span>
                    <span className="text-muted-foreground text-sm">{isExpanded ? '▲' : '▼'}</span>
                  </button>
                  {isExpanded && (
                    <div className="px-4 pb-4 text-sm whitespace-pre-wrap text-muted-foreground border-t pt-3">
                      {block.content}
                    </div>
                  )}
                </div>
              )
            })
          ) : report.content_md ? (
            <div className="border rounded-lg p-4 text-sm whitespace-pre-wrap text-muted-foreground">
              {report.content_md}
            </div>
          ) : null}
        </div>
      )}
    </div>
  )
}
