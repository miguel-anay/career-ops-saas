'use client'

import { useEffect, useState, useRef, useCallback } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { postIngest, getIngestion, type IngestionStatus } from '@/features/cv/api'
import { useJobProgress } from '@/features/cv/hooks'

const POLL_INTERVAL_MS = 4000

export default function IngestCVPage() {
  const [rawCV, setRawCV] = useState('')
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [runId, setRunId] = useState<string | null>(null)
  const [pollResult, setPollResult] = useState<IngestionStatus | null>(null)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const { status, payload, isConnected, connect, reset } = useJobProgress()

  const stopPolling = useCallback(() => {
    if (pollRef.current) {
      clearInterval(pollRef.current)
      pollRef.current = null
    }
  }, [])

  // Polling fallback: when a run is in flight (status not idle) but the WS
  // is not connected, fall back to GET /api/cv/ingest/:id.
  useEffect(() => {
    if (!runId) return
    if (status === 'idle' || status === 'completed' || status === 'error') {
      stopPolling()
      return
    }
    if (isConnected) {
      stopPolling()
      return
    }
    if (pollRef.current) return

    pollRef.current = setInterval(async () => {
      try {
        const result = await getIngestion(runId)
        setPollResult(result)
        if (result.status === 'completed' || result.status === 'failed') {
          stopPolling()
        }
      } catch {
        // Keep polling — a transient failure shouldn't kill the fallback loop
      }
    }, POLL_INTERVAL_MS)

    return () => stopPolling()
  }, [runId, status, isConnected, stopPolling])

  useEffect(() => {
    return () => stopPolling()
  }, [stopPolling])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!rawCV.trim()) return

    setIsSubmitting(true)
    setPollResult(null)
    reset()
    try {
      const { run_id } = await postIngest(rawCV.trim())
      setRunId(run_id)
      connect(run_id)
    } catch {
      toast.error('Failed to submit CV')
    } finally {
      setIsSubmitting(false)
    }
  }

  const effectiveStatus = pollResult?.status === 'completed'
    ? 'completed'
    : pollResult?.status === 'failed'
      ? 'error'
      : status

  return (
    <div className="container mx-auto p-6 space-y-6 max-w-2xl">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Ingest CV</h1>
      </div>

      <form onSubmit={handleSubmit} className="space-y-4">
        <Textarea
          placeholder="Paste your CV text here…"
          value={rawCV}
          onChange={e => setRawCV(e.target.value)}
          rows={12}
        />
        <Button type="submit" disabled={isSubmitting || !rawCV.trim()}>
          {isSubmitting ? 'Submitting…' : 'Submit'}
        </Button>
      </form>

      {effectiveStatus !== 'idle' && (
        <div className="rounded border p-4 text-sm space-y-2">
          {effectiveStatus === 'connecting' && <p>Connecting…</p>}
          {effectiveStatus === 'working' && <p>Processing your CV…</p>}
          {effectiveStatus === 'completed' && (
            <div>
              <p className="font-medium text-green-700">Completed</p>
              {payload && (
                <pre className="text-xs whitespace-pre-wrap break-words">
                  {JSON.stringify(payload, null, 2)}
                </pre>
              )}
            </div>
          )}
          {effectiveStatus === 'error' && (
            <div>
              <p className="font-medium text-red-700">Failed</p>
              {payload && (
                <pre className="text-xs whitespace-pre-wrap break-words">
                  {JSON.stringify(payload, null, 2)}
                </pre>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
