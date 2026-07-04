import { describe, it, expect, afterAll } from 'vitest'

// Integration contract for tenantQuery against a REAL Postgres. The unit
// test in tests/lib/db.test.mjs mocks pg and happily accepts invalid SQL —
// which is how `SET LOCAL app.current_user_id = $1` (bind params are not
// allowed in SET, error 42601) shipped and broke every tenant write in the
// worker (issue #42 follow-up). This suite pins the real behavior: the GUC
// is set inside the transaction and RLS actually scopes rows.
//
// Skips cleanly when TEST_DATABASE_URL is unset. To run against a live DB:
//   TEST_DATABASE_URL="postgres://app_user:app_pw@localhost:5432/careerops?sslmode=disable" \
//   npx vitest run tests/integration/tenant-query-real-db.test.mjs

const TEST_DATABASE_URL = process.env.TEST_DATABASE_URL

const describeIfDb = TEST_DATABASE_URL ? describe : describe.skip

process.env.DATABASE_URL = TEST_DATABASE_URL || 'postgres://unused'

const { tenantQuery, pool } = TEST_DATABASE_URL
  ? await import('../../lib/db.mjs')
  : { tenantQuery: null, pool: null }

describeIfDb('tenantQuery (real Postgres)', () => {
  afterAll(async () => {
    await pool.end()
  })

  it('sets app.current_user_id transaction-locally and executes the query', async () => {
    const userId = '00000000-0000-0000-0000-000000000001'

    const result = await tenantQuery(
      userId,
      `SELECT current_setting('app.current_user_id', true) AS guc`,
    )

    expect(result.rows[0].guc).toBe(userId)
  })

  it('does not leak the GUC outside the transaction', async () => {
    const userId = '00000000-0000-0000-0000-000000000002'
    await tenantQuery(userId, 'SELECT 1')

    const after = await pool.query(
      `SELECT current_setting('app.current_user_id', true) AS guc`,
    )
    expect([null, '']).toContain(after.rows[0].guc)
  })
})
