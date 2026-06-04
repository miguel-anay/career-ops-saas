import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'

// Mock WebSocket
class MockWebSocket {
  static instances: MockWebSocket[] = []
  url: string
  readyState: number = 0 // CONNECTING
  onopen: ((e: Event) => void) | null = null
  onmessage: ((e: MessageEvent) => void) | null = null
  onclose: ((e: CloseEvent) => void) | null = null
  onerror: ((e: Event) => void) | null = null

  constructor(url: string) {
    this.url = url
    MockWebSocket.instances.push(this)
    // Simulate async open
    setTimeout(() => {
      this.readyState = 1 // OPEN
      this.onopen?.(new Event('open'))
    }, 0)
  }

  close() {
    this.readyState = 3 // CLOSED
    this.onclose?.(new CloseEvent('close'))
  }

  simulateMessage(data: unknown) {
    this.onmessage?.(new MessageEvent('message', { data: JSON.stringify(data) }))
  }
}

// Attach OPEN/CONNECTING/CLOSED constants to mock
Object.assign(MockWebSocket, { OPEN: 1, CONNECTING: 0, CLOSED: 3 })

const localStorageMock = (() => {
  let store: Record<string, string> = {}
  return {
    getItem: (key: string) => store[key] ?? null,
    setItem: (key: string, value: string) => { store[key] = value },
    removeItem: (key: string) => { delete store[key] },
    clear: () => { store = {} },
  }
})()

Object.defineProperty(global, 'localStorage', {
  value: localStorageMock,
  writable: true,
})

beforeEach(() => {
  MockWebSocket.instances = []
  localStorageMock.clear()
  localStorageMock.setItem('access_token', 'test-token')
  vi.stubGlobal('WebSocket', MockWebSocket)
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useScanProgress', () => {
  it('connects with correct URL including token when connect() is called', async () => {
    const { useScanProgress } = await import('../../hooks/useScanProgress')
    const { result } = renderHook(() => useScanProgress())

    act(() => {
      result.current.connect()
    })

    expect(MockWebSocket.instances).toHaveLength(1)
    expect(MockWebSocket.instances[0].url).toContain('token=test-token')
  })

  it('parses incoming JSON events correctly', async () => {
    const { useScanProgress } = await import('../../hooks/useScanProgress')
    const { result } = renderHook(() => useScanProgress())

    await act(async () => {
      result.current.connect()
      // Wait for open
      await new Promise(r => setTimeout(r, 10))
      const ws = MockWebSocket.instances[0]
      ws.simulateMessage({ event: 'scan.job_found', data: { job_id: '1', title: 'Engineer', company: 'Acme', url: 'http://a.com', is_new: true } })
    })

    expect(result.current.events).toHaveLength(1)
    expect(result.current.events[0].event).toBe('scan.job_found')
  })

  it('status updates on scan.completed event', async () => {
    const { useScanProgress } = await import('../../hooks/useScanProgress')
    const { result } = renderHook(() => useScanProgress())

    await act(async () => {
      result.current.connect()
      await new Promise(r => setTimeout(r, 10))
      const ws = MockWebSocket.instances[0]
      ws.simulateMessage({ event: 'scan.completed', data: { status: 'completed', new_jobs: 5 } })
    })

    expect(result.current.status).toBe('completed')
  })

  it('cleanup closes WebSocket on unmount', async () => {
    const { useScanProgress } = await import('../../hooks/useScanProgress')
    const { result, unmount } = renderHook(() => useScanProgress())

    await act(async () => {
      result.current.connect()
      await new Promise(r => setTimeout(r, 10))
    })

    unmount()

    expect(MockWebSocket.instances[0].readyState).toBe(3) // CLOSED
  })
})
