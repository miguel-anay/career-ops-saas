import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'

// Mock WebSocket (mirrors __tests__/hooks/useScanProgress.test.ts)
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

const RUN_ID = '11111111-1111-1111-1111-111111111111'

describe('useJobProgress', () => {
  it('connect(runId) transitions idle -> connecting -> working and includes scan_run_id + token in the URL', async () => {
    const { useJobProgress } = await import('../../hooks/useJobProgress')
    const { result } = renderHook(() => useJobProgress())

    expect(result.current.status).toBe('idle')

    act(() => {
      result.current.connect(RUN_ID)
    })

    expect(result.current.status).toBe('connecting')
    expect(MockWebSocket.instances).toHaveLength(1)
    expect(MockWebSocket.instances[0].url).toContain('token=test-token')
    expect(MockWebSocket.instances[0].url).toContain(`scan_run_id=${RUN_ID}`)

    await act(async () => {
      await new Promise(r => setTimeout(r, 10))
    })

    expect(result.current.status).toBe('working')
  })

  it('transitions to completed and surfaces payload on ingest.completed', async () => {
    const { useJobProgress } = await import('../../hooks/useJobProgress')
    const { result } = renderHook(() => useJobProgress())

    await act(async () => {
      result.current.connect(RUN_ID)
      await new Promise(r => setTimeout(r, 10))
      const ws = MockWebSocket.instances[0]
      ws.simulateMessage({
        event: 'ingest.completed',
        data: { run_id: RUN_ID, status: 'completed', profile_json: { candidate: { full_name: 'Ada Lovelace' } } },
      })
    })

    expect(result.current.status).toBe('completed')
    expect(result.current.payload).toEqual({
      run_id: RUN_ID,
      status: 'completed',
      profile_json: { candidate: { full_name: 'Ada Lovelace' } },
    })
  })

  it('transitions to error and surfaces diagnostic payload on ingest.failed', async () => {
    const { useJobProgress } = await import('../../hooks/useJobProgress')
    const { result } = renderHook(() => useJobProgress())

    await act(async () => {
      result.current.connect(RUN_ID)
      await new Promise(r => setTimeout(r, 10))
      const ws = MockWebSocket.instances[0]
      ws.simulateMessage({
        event: 'ingest.failed',
        data: { run_id: RUN_ID, status: 'failed', error: 'anthropic_error' },
      })
    })

    expect(result.current.status).toBe('error')
    expect(result.current.payload).toEqual({
      run_id: RUN_ID,
      status: 'failed',
      error: 'anthropic_error',
    })
  })

  it('reconnects once after an unexpected close while still working', async () => {
    const { useJobProgress } = await import('../../hooks/useJobProgress')
    const { result } = renderHook(() => useJobProgress())

    await act(async () => {
      result.current.connect(RUN_ID)
      await new Promise(r => setTimeout(r, 10))
    })

    expect(MockWebSocket.instances).toHaveLength(1)

    await act(async () => {
      MockWebSocket.instances[0].close()
      await new Promise(r => setTimeout(r, 1100))
    })

    // Reconnect attempted exactly once -> a second WS instance was created
    expect(MockWebSocket.instances).toHaveLength(2)

    await act(async () => {
      MockWebSocket.instances[1].close()
      await new Promise(r => setTimeout(r, 1100))
    })

    // No further reconnect attempts beyond the first
    expect(MockWebSocket.instances).toHaveLength(2)
  })

  it('does not cross-deliver events between two different run_ids', async () => {
    const { useJobProgress } = await import('../../hooks/useJobProgress')
    const { result: resultX } = renderHook(() => useJobProgress())
    const { result: resultY } = renderHook(() => useJobProgress())

    const RUN_X = '22222222-2222-2222-2222-222222222222'
    const RUN_Y = '33333333-3333-3333-3333-333333333333'

    await act(async () => {
      resultX.current.connect(RUN_X)
      await new Promise(r => setTimeout(r, 10))
    })
    await act(async () => {
      resultY.current.connect(RUN_Y)
      await new Promise(r => setTimeout(r, 10))
    })

    expect(MockWebSocket.instances).toHaveLength(2)
    const wsX = MockWebSocket.instances.find(i => i.url.includes(RUN_X))!
    const wsY = MockWebSocket.instances.find(i => i.url.includes(RUN_Y))!

    await act(async () => {
      wsX.simulateMessage({ event: 'ingest.completed', data: { run_id: RUN_X, status: 'completed' } })
    })

    expect(resultX.current.status).toBe('completed')
    // Y's connection received nothing -> still working, no payload
    expect(resultY.current.status).toBe('working')
    expect(resultY.current.payload).toBeNull()
    void wsY
  })

  it('reset() returns the hook to idle and clears payload', async () => {
    const { useJobProgress } = await import('../../hooks/useJobProgress')
    const { result } = renderHook(() => useJobProgress())

    await act(async () => {
      result.current.connect(RUN_ID)
      await new Promise(r => setTimeout(r, 10))
      const ws = MockWebSocket.instances[0]
      ws.simulateMessage({ event: 'ingest.completed', data: { run_id: RUN_ID, status: 'completed' } })
    })

    expect(result.current.status).toBe('completed')

    act(() => {
      result.current.reset()
    })

    expect(result.current.status).toBe('idle')
    expect(result.current.payload).toBeNull()
  })

  it('cleanup closes the WebSocket on unmount', async () => {
    const { useJobProgress } = await import('../../hooks/useJobProgress')
    const { result, unmount } = renderHook(() => useJobProgress())

    await act(async () => {
      result.current.connect(RUN_ID)
      await new Promise(r => setTimeout(r, 10))
    })

    unmount()

    expect(MockWebSocket.instances[0].readyState).toBe(3) // CLOSED
  })
})
