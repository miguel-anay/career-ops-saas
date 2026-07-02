'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { apiGet, apiPost } from '@/lib/api'

const TERMINAL_STATUSES = new Set(['completed', 'partial', 'error'])
const DEFAULT_POLL_INTERVAL_MS = 2000

interface EmailIngestRun {
  id: string
  status: 'running' | 'completed' | 'partial' | 'error'
  new_jobs?: number
}

interface EmailIngestButtonsProps {
  /** Called once a sync run reaches a non-error terminal state (new jobs may be available). */
  onSynced?: () => void
  /** Test seam — production uses the default. */
  pollIntervalMs?: number
}

export function EmailIngestButtons({ onSynced, pollIntervalMs = DEFAULT_POLL_INTERVAL_MS }: EmailIngestButtonsProps) {
  const [status, setStatus] = useState<string | null>(null)
  const [isSyncing, setIsSyncing] = useState(false)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const stopPolling = useCallback(() => {
    if (timerRef.current) {
      clearInterval(timerRef.current)
      timerRef.current = null
    }
  }, [])

  useEffect(() => stopPolling, [stopPolling])

  const handleConnectGmail = async () => {
    try {
      // Bearer-authenticated endpoint — must go through apiGet (attaches the
      // token, handles refresh), never a plain window.location navigation
      // (which sends no Authorization header and would always 401).
      const { auth_url } = await apiGet<{ auth_url: string }>('/auth/google/gmail')
      window.location.href = auth_url
    } catch {
      toast.error('Failed to start Gmail connection')
    }
  }

  const pollRun = useCallback((runId: string) => {
    timerRef.current = setInterval(async () => {
      try {
        const run = await apiGet<EmailIngestRun>(`/api/email-ingest-runs/${runId}`)
        setStatus(run.status)
        if (!TERMINAL_STATUSES.has(run.status)) return

        stopPolling()
        setIsSyncing(false)
        if (run.status === 'error') {
          toast.error('Email sync failed')
        } else {
          toast.success(`Email sync ${run.status} — ${run.new_jobs ?? 0} new job${run.new_jobs === 1 ? '' : 's'}`)
          onSynced?.()
        }
      } catch {
        stopPolling()
        setIsSyncing(false)
        toast.error('Failed to check email sync status')
      }
    }, pollIntervalMs)
  }, [onSynced, pollIntervalMs, stopPolling])

  const handleSync = async () => {
    setIsSyncing(true)
    setStatus('running')
    try {
      const { ingest_run_id } = await apiPost<{ ingest_run_id: string }>('/api/email-ingest', {})
      pollRun(ingest_run_id)
    } catch (err) {
      setIsSyncing(false)
      setStatus(null)
      const message = err instanceof Error && err.message.includes('gmail_not_connected')
        ? 'Connect Gmail first'
        : 'Failed to start email sync'
      toast.error(message)
    }
  }

  return (
    <div className="flex items-center gap-2">
      <Button variant="outline" onClick={handleConnectGmail}>Connect Gmail</Button>
      <Button variant="outline" onClick={handleSync} disabled={isSyncing}>
        {isSyncing ? 'Syncing…' : 'Sync email alerts'}
      </Button>
      {status && <span className="text-sm text-muted-foreground">{status}</span>}
    </div>
  )
}
