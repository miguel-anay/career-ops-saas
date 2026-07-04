import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest'

// Mock pg before importing db
const mockQuery = vi.fn()
const mockRelease = vi.fn()
const mockConnect = vi.fn()

vi.mock('pg', () => {
  class MockPool {
    connect() {
      return Promise.resolve(mockConnect())
    }
    end() {
      return Promise.resolve()
    }
  }
  return { default: { Pool: MockPool }, Pool: MockPool }
})

// Import after mocking
const { tenantQuery } = await import('../../lib/db.mjs')

describe('tenantQuery', () => {
  let mockClient

  beforeEach(() => {
    mockClient = {
      query: mockQuery,
      release: mockRelease,
    }
    mockConnect.mockReturnValue(mockClient)
    mockQuery.mockResolvedValue({ rows: [], rowCount: 0 })
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  it('sets app.current_user_id via set_config before the main query', async () => {
    const userId = 'user-123'
    const sql = 'SELECT * FROM jobs WHERE user_id = current_setting(\'app.current_user_id\')::uuid'
    const params = []

    await tenantQuery(userId, sql, params)

    // First call should be BEGIN
    expect(mockQuery).toHaveBeenNthCalledWith(1, 'BEGIN')
    // Second call sets the tenant GUC. Must be set_config($1, ..., true):
    // SET LOCAL rejects bind parameters (42601) — pinned by the real-DB
    // integration test in tests/integration/tenant-query-real-db.test.mjs.
    expect(mockQuery).toHaveBeenNthCalledWith(2, `SELECT set_config('app.current_user_id', $1, true)`, [userId])
    // Third call should be the actual query
    expect(mockQuery).toHaveBeenNthCalledWith(3, sql, params)
    // Fourth call should be COMMIT
    expect(mockQuery).toHaveBeenNthCalledWith(4, 'COMMIT')
  })

  it('releases client after successful query', async () => {
    await tenantQuery('user-1', 'SELECT 1', [])
    expect(mockRelease).toHaveBeenCalledOnce()
  })

  it('rolls back and releases on error', async () => {
    mockQuery
      .mockResolvedValueOnce(undefined)  // BEGIN
      .mockResolvedValueOnce(undefined)  // SET LOCAL
      .mockRejectedValueOnce(new Error('DB error'))  // main query

    await expect(tenantQuery('user-1', 'SELECT fail', [])).rejects.toThrow('DB error')

    expect(mockQuery).toHaveBeenCalledWith('ROLLBACK')
    expect(mockRelease).toHaveBeenCalledOnce()
  })

  it('returns the result of the main query', async () => {
    const expectedRows = [{ id: '1', title: 'Software Engineer' }]
    mockQuery
      .mockResolvedValueOnce(undefined)  // BEGIN
      .mockResolvedValueOnce(undefined)  // SET LOCAL
      .mockResolvedValueOnce({ rows: expectedRows, rowCount: 1 })  // main query
      .mockResolvedValueOnce(undefined)  // COMMIT

    const result = await tenantQuery('user-1', 'SELECT * FROM jobs', [])
    expect(result.rows).toEqual(expectedRows)
  })
})
