'use client'

import { useEffect, useState, useCallback } from 'react'
import Link from 'next/link'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { apiGet, apiPatch } from '@/lib/api'

interface Application {
  id: string
  job_id: string
  company: string
  role: string
  score?: number
  status: string
  notes?: string
  applied_at: string
  pdf_path?: string | null
}

interface ApplicationsResponse {
  applications: Application[]
  total: number
}

const STATUSES = ['Evaluated', 'Applied', 'Responded', 'Interview', 'Offer', 'Rejected', 'Discarded', 'SKIP']

function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
}

export default function TrackerPage() {
  const [applications, setApplications] = useState<Application[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [isLoading, setIsLoading] = useState(true)
  const [editingNotes, setEditingNotes] = useState<Record<string, string>>({})

  const loadApplications = useCallback(async (p = 1) => {
    setIsLoading(true)
    try {
      const data = await apiGet<ApplicationsResponse>(`/api/applications?page=${p}&limit=20`)
      setApplications(data.applications)
      setTotal(data.total)
      setPage(p)
    } catch {
      toast.error('Failed to load applications')
    } finally {
      setIsLoading(false)
    }
  }, [])

  useEffect(() => {
    loadApplications()
  }, [loadApplications])

  const handleStatusChange = async (id: string, newStatus: string) => {
    try {
      await apiPatch(`/api/applications/${id}`, { status: newStatus })
      setApplications(prev =>
        prev.map(a => a.id === id ? { ...a, status: newStatus } : a)
      )
    } catch {
      toast.error('Failed to update status')
    }
  }

  const handleNotesBlur = async (id: string) => {
    const notes = editingNotes[id]
    if (notes === undefined) return
    try {
      await apiPatch(`/api/applications/${id}`, { notes })
      setApplications(prev =>
        prev.map(a => a.id === id ? { ...a, notes } : a)
      )
      toast.success('Notes saved')
    } catch {
      toast.error('Failed to save notes')
    }
  }

  const pageCount = Math.ceil(total / 20)

  return (
    <div className="container mx-auto p-6 space-y-6">
      <h1 className="text-2xl font-bold">Application Tracker</h1>

      <div className="rounded-md border overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Company</TableHead>
              <TableHead>Role</TableHead>
              <TableHead>Score</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>PDF</TableHead>
              <TableHead className="min-w-[180px]">Notes</TableHead>
              <TableHead>Date</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableRow>
                <TableCell colSpan={7} className="text-center py-8 text-muted-foreground">
                  Loading…
                </TableCell>
              </TableRow>
            ) : applications.length === 0 ? (
              <TableRow>
                <TableCell colSpan={7} className="text-center py-8 text-muted-foreground">
                  No applications yet.
                </TableCell>
              </TableRow>
            ) : (
              applications.map(app => (
                <TableRow key={app.id}>
                  <TableCell className="font-medium">{app.company}</TableCell>
                  <TableCell>
                    <Link href={`/jobs/${app.job_id}`} className="hover:underline">
                      {app.role}
                    </Link>
                  </TableCell>
                  <TableCell>
                    {app.score !== undefined && app.score !== null ? (
                      <span
                        className={
                          app.score >= 4 ? 'text-green-600 font-semibold' :
                          app.score >= 3 ? 'text-yellow-600 font-semibold' :
                          'text-red-600 font-semibold'
                        }
                      >
                        {app.score.toFixed(1)}
                      </span>
                    ) : '—'}
                  </TableCell>
                  <TableCell>
                    <select
                      value={app.status}
                      onChange={e => handleStatusChange(app.id, e.target.value)}
                      className="border rounded px-2 py-1 text-sm bg-background"
                    >
                      {STATUSES.map(s => (
                        <option key={s} value={s}>{s}</option>
                      ))}
                    </select>
                  </TableCell>
                  <TableCell>
                    {app.pdf_path ? (
                      <a
                        href={app.pdf_path}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="text-blue-600 hover:underline text-sm"
                      >
                        Download
                      </a>
                    ) : (
                      <span className="text-muted-foreground text-sm">N/A</span>
                    )}
                  </TableCell>
                  <TableCell>
                    <Textarea
                      value={editingNotes[app.id] ?? app.notes ?? ''}
                      onChange={e =>
                        setEditingNotes(prev => ({ ...prev, [app.id]: e.target.value }))
                      }
                      onBlur={() => handleNotesBlur(app.id)}
                      rows={2}
                      className="text-sm min-h-0 resize-none"
                      placeholder="Notes…"
                    />
                  </TableCell>
                  <TableCell className="text-sm text-muted-foreground whitespace-nowrap">
                    {formatDate(app.applied_at)}
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
            onClick={() => loadApplications(page - 1)}
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
            onClick={() => loadApplications(page + 1)}
          >
            Next
          </Button>
        </div>
      )}
    </div>
  )
}
