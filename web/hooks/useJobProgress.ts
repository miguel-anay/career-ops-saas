'use client'

import { useState, useRef, useCallback, useEffect } from 'react'
import { getAccessToken } from '@/lib/auth'

const WS_URL = process.env.NEXT_PUBLIC_WS_URL ?? 'ws://localhost:8080'

export type JobProgressStatus = 'idle' | 'connecting' | 'working' | 'completed' | 'error'

export interface JobProgressPayload {
  [key: string]: unknown
}

interface JobProgressEvent {
  event: string
  data: JobProgressPayload
}

interface UseJobProgressReturn {
  status: JobProgressStatus
  payload: JobProgressPayload | null
  isConnected: boolean
  error: string | null
  connect: (runId: string) => void
  reset: () => void
}

/**
 * Generalized clone of useScanProgress for any job that reports terminal
 * progress over the shared /ws/scan WS route, keyed by run_id (renamed from
 * scan_run_id in the NOTIFY envelope — the query param name itself is
 * intentionally unchanged, see design.md Decision D4).
 */
export function useJobProgress(): UseJobProgressReturn {
  const [status, setStatus] = useState<JobProgressStatus>('idle')
  const [payload, setPayload] = useState<JobProgressPayload | null>(null)
  const [isConnected, setIsConnected] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectAttempted = useRef(false)
  const statusRef = useRef<JobProgressStatus>('idle')
  const runIdRef = useRef<string | null>(null)

  const cleanup = useCallback(() => {
    if (wsRef.current) {
      wsRef.current.onopen = null
      wsRef.current.onmessage = null
      wsRef.current.onclose = null
      wsRef.current.onerror = null
      if (wsRef.current.readyState === WebSocket.OPEN || wsRef.current.readyState === WebSocket.CONNECTING) {
        wsRef.current.close()
      }
      wsRef.current = null
    }
  }, [])

  const doConnect = useCallback((runId: string) => {
    cleanup()
    runIdRef.current = runId

    const token = getAccessToken()
    const params = new URLSearchParams({ scan_run_id: runId })
    if (token) params.set('token', token)
    const url = `${WS_URL}/ws/scan?${params.toString()}`

    statusRef.current = 'connecting'
    setStatus('connecting')
    setError(null)

    const ws = new WebSocket(url)
    wsRef.current = ws

    ws.onopen = () => {
      setIsConnected(true)
      statusRef.current = 'working'
      setStatus('working')
    }

    ws.onmessage = (event) => {
      try {
        const parsed: JobProgressEvent = JSON.parse(event.data)

        if (parsed.event === 'ingest.completed') {
          statusRef.current = 'completed'
          setStatus('completed')
          setPayload(parsed.data)
        } else if (parsed.event === 'ingest.failed') {
          statusRef.current = 'error'
          setStatus('error')
          setPayload(parsed.data)
        }
      } catch {
        // Ignore malformed messages
      }
    }

    ws.onclose = () => {
      setIsConnected(false)
      // Auto-reconnect once, unless we've already reached a terminal state
      if (
        !reconnectAttempted.current &&
        statusRef.current !== 'completed' &&
        statusRef.current !== 'error' &&
        runIdRef.current
      ) {
        reconnectAttempted.current = true
        const idToRetry = runIdRef.current
        setTimeout(() => doConnect(idToRetry), 1000)
      }
    }

    ws.onerror = () => {
      setError('WebSocket connection error')
      statusRef.current = 'error'
      setStatus('error')
      setIsConnected(false)
    }
  }, [cleanup])

  const connect = useCallback((runId: string) => {
    reconnectAttempted.current = false
    doConnect(runId)
  }, [doConnect])

  const reset = useCallback(() => {
    cleanup()
    statusRef.current = 'idle'
    setStatus('idle')
    setPayload(null)
    setIsConnected(false)
    setError(null)
    reconnectAttempted.current = false
    runIdRef.current = null
  }, [cleanup])

  useEffect(() => {
    return () => {
      cleanup()
    }
  }, [cleanup])

  return { status, payload, isConnected, error, connect, reset }
}
