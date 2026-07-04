'use client'

import { useEffect, useState, useCallback } from 'react'
import Link from 'next/link'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { apiGet, apiPost } from '@/lib/api'
import { isAuthenticated } from '@/lib/auth'
import { EmailIngestButtons } from '@/features/email-ingest/EmailIngestButtons'
import { useScanProgress } from './hooks'
import type { Job, JobsResponse } from './types'

const STATUS_COLORS: Record<string, 'default' | 'secondary' | 'outline' | 'destructive'> = {
  pending: 'secondary',
  evaluating: 'outline',
  evaluated: 'default',
  error: 'destructive',
}

function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
}

export function JobsDashboard() {
  const [jobs, setJobs] = useState<Job[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [isLoading, setIsLoading] = useState(true)
  const [jobUrl, setJobUrl] = useState('')
  const [isAdding, setIsAdding] = useState(false)
  const [isScanning, setIsScanning] = useState(false)

  const { events, status: scanStatus, connect, reset } = useScanProgress()

  const loadJobs = useCallback(async (p = 1) => {
    setIsLoading(true)
    try {
      const data = await apiGet<JobsResponse>(`/api/jobs?page=${p}&limit=20`)
      setJobs(data.jobs ?? [])
      setTotal(data.total ?? 0)
      setPage(p)
    } catch {
      toast.error('Failed to load jobs')
    } finally {
      setIsLoading(false)
    }
  }, [])

  useEffect(() => {
    // Preserve original behavior: no data fetch when unauthenticated (the
    // route composer handles the redirect). Decoupling these would fire an
    // API call that 401s before the redirect lands.
    if (!isAuthenticated()) return
    loadJobs()
  }, [loadJobs])

  // Handle scan WebSocket events
  useEffect(() => {
    const lastEvent = events[events.length - 1]
    if (!lastEvent) return

    if (lastEvent.event === 'scan.company.done') {
      const d = lastEvent.data as { company: string; new: number; found: number }
      if (d.new > 0) {
        toast.success(`Found ${d.new} new job${d.new !== 1 ? 's' : ''} at ${d.company}`)
      }
    } else if (lastEvent.event === 'scan.company.error') {
      const d = lastEvent.data as { company: string; error: string }
      toast.error(`Failed to scan ${d.company}: ${d.error}`)
    } else if (lastEvent.event === 'scan.job_found') {
      const d = lastEvent.data as unknown as Job & { is_new: boolean }
      if (d.is_new) {
        setJobs(prev => {
          const exists = prev.some(j => j.id === d.id)
          if (exists) return prev
          return [d, ...prev]
        })
      }
    } else if (lastEvent.event === 'scan.completed') {
      const d = lastEvent.data as { status: string; new_jobs: number }
      toast.success(`Scan complete — ${d.new_jobs} new job${d.new_jobs !== 1 ? 's' : ''} found`)
      setIsScanning(false)
      loadJobs()
    }
  }, [events, loadJobs])

  const handleAddJob = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!jobUrl.trim()) return
    setIsAdding(true)
    try {
      await apiPost('/api/jobs', { url: jobUrl.trim() })
      setJobUrl('')
      toast.success('Job added successfully')
      loadJobs()
    } catch {
      toast.error('Failed to add job')
    } finally {
      setIsAdding(false)
    }
  }

  const handleScanNow = async () => {
    setIsScanning(true)
    reset()
    try {
      await apiPost('/api/scan', {})
      connect()
    } catch {
      toast.error('Failed to start scan')
      setIsScanning(false)
    }
  }

  const pageCount = Math.ceil(total / 20)

  return (
    <div className="container mx-auto p-6 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Job Pipeline</h1>
        <div className="flex gap-2">
          <Link href="/tracker">
            <Button variant="outline">Tracker</Button>
          </Link>
          <Link href="/companies">
            <Button variant="outline">Companies</Button>
          </Link>
          <Button onClick={handleScanNow} disabled={isScanning}>
            {isScanning ? 'Scanning…' : 'Scan Now'}
          </Button>
          <EmailIngestButtons onSynced={() => loadJobs()} />
        </div>
      </div>

      {/* Add Job URL form */}
      <form onSubmit={handleAddJob} className="flex gap-2">
        <Input
          type="url"
          placeholder="Job URL (e.g. https://greenhouse.io/jobs/...)"
          value={jobUrl}
          onChange={e => setJobUrl(e.target.value)}
          className="flex-1"
        />
        <Button type="submit" disabled={isAdding || !jobUrl.trim()}>
          {isAdding ? 'Adding…' : 'Add'}
        </Button>
      </form>

      {/* Scan status banner */}
      {isScanning && (
        <div className="bg-blue-50 border border-blue-200 rounded p-3 text-sm text-blue-800">
          Scanning portals… {scanStatus === 'scanning' ? '(in progress)' : scanStatus}
        </div>
      )}

      {/* Jobs table */}
      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Title</TableHead>
              <TableHead>Company</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Score</TableHead>
              <TableHead>Received</TableHead>
              <TableHead className="w-[80px]">Action</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableRow>
                <TableCell colSpan={6} className="text-center py-8 text-muted-foreground">
                  Loading…
                </TableCell>
              </TableRow>
            ) : jobs.length === 0 ? (
              <TableRow>
                <TableCell colSpan={6} className="text-center py-8 text-muted-foreground">
                  No jobs yet. Add a URL or run a scan.
                </TableCell>
              </TableRow>
            ) : (
              jobs.map(job => (
                <TableRow key={job.id}>
                  <TableCell className="font-medium">
                    <Link href={`/jobs/${job.id}`} className="hover:underline">
                      {job.title || '(untitled)'}
                    </Link>
                  </TableCell>
                  <TableCell>{job.company}</TableCell>
                  <TableCell>
                    <Badge variant={STATUS_COLORS[job.status] ?? 'secondary'}>
                      {job.status}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    {job.score !== undefined && job.score !== null ? (
                      <span
                        className={
                          job.score >= 4 ? 'text-green-600 font-semibold' :
                          job.score >= 3 ? 'text-yellow-600 font-semibold' :
                          'text-red-600 font-semibold'
                        }
                      >
                        {job.score.toFixed(1)}
                      </span>
                    ) : '—'}
                  </TableCell>
                  <TableCell className="text-sm text-muted-foreground">
                    {formatDate(job.received_at)}
                  </TableCell>
                  <TableCell>
                    <Link href={`/jobs/${job.id}`}>
                      <Button variant="ghost" size="sm">View</Button>
                    </Link>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>

      {/* Pagination */}
      {pageCount > 1 && (
        <div className="flex justify-center gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={page <= 1}
            onClick={() => loadJobs(page - 1)}
          >
            Previous
          </Button>
          <span className="text-sm text-muted-foreground self-center">
            Page {page} of {pageCount}
          </span>
          <Button
            variant="outline"
            size="sm"
            disabled={page >= pageCount}
            onClick={() => loadJobs(page + 1)}
          >
            Next
          </Button>
        </div>
      )}
    </div>
  )
}
