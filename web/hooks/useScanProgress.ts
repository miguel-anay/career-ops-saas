'use client'

import { useState, useEffect, useRef, useCallback } from 'react'
import { getAccessToken } from '@/lib/auth'

const WS_URL = process.env.NEXT_PUBLIC_WS_URL ?? 'ws://localhost:8080'

export interface ScanEvent {
  event: string
  data: Record<string, unknown>
}

type ScanStatus = 'idle' | 'connecting' | 'scanning' | 'completed' | 'partial' | 'error'

interface UseScanProgressReturn {
  events: ScanEvent[]
  status: ScanStatus
  isConnected: boolean
  error: string | null
  connect: () => void
  reset: () => void
}

export function useScanProgress(): UseScanProgressReturn {
  const [events, setEvents] = useState<ScanEvent[]>([])
  const [status, setStatus] = useState<ScanStatus>('idle')
  const [isConnected, setIsConnected] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectAttempted = useRef(false)

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

  const connect = useCallback(() => {
    cleanup()
    reconnectAttempted.current = false

    const token = getAccessToken()
    const url = `${WS_URL}/ws/scan${token ? `?token=${token}` : ''}`

    setStatus('connecting')
    setError(null)

    const ws = new WebSocket(url)
    wsRef.current = ws

    ws.onopen = () => {
      setIsConnected(true)
      setStatus('scanning')
    }

    ws.onmessage = (event) => {
      try {
        const parsed: ScanEvent = JSON.parse(event.data)
        setEvents(prev => [...prev, parsed])

        if (parsed.event === 'scan.completed') {
          const scanStatus = (parsed.data as { status?: string }).status
          setStatus(scanStatus === 'partial' ? 'partial' : 'completed')
        } else if (parsed.event === 'scan.started') {
          setStatus('scanning')
        }
      } catch {
        // Ignore malformed messages
      }
    }

    ws.onclose = () => {
      setIsConnected(false)
      // Auto-reconnect once
      if (!reconnectAttempted.current && status !== 'completed' && status !== 'partial') {
        reconnectAttempted.current = true
        setTimeout(connect, 1000)
      }
    }

    ws.onerror = () => {
      setError('WebSocket connection error')
      setStatus('error')
      setIsConnected(false)
    }
  }, [cleanup, status])

  const reset = useCallback(() => {
    cleanup()
    setEvents([])
    setStatus('idle')
    setIsConnected(false)
    setError(null)
    reconnectAttempted.current = false
  }, [cleanup])

  useEffect(() => {
    return () => {
      cleanup()
    }
  }, [cleanup])

  return { events, status, isConnected, error, connect, reset }
}
