import pg from 'pg'

const { Pool } = pg

export const pool = new Pool({
  connectionString: process.env.DATABASE_URL,
})

/**
 * Execute a SQL query within a transaction with RLS tenant isolation.
 *
 * Sets `app.current_user_id` as a LOCAL variable before running the query,
 * which is picked up by PostgreSQL RLS policies. The entire operation runs
 * inside a BEGIN/COMMIT block so the SET LOCAL is scoped to the transaction.
 *
 * @param {string} userId - UUID of the tenant user (used for RLS)
 * @param {string} sql - SQL query to execute
 * @param {any[]} params - Query parameters
 * @returns {Promise<import('pg').QueryResult>}
 */
export async function tenantQuery(userId, sql, params = []) {
  const client = await pool.connect()
  try {
    await client.query('BEGIN')
    // SET LOCAL cannot take bind parameters (Postgres 42601); set_config with
    // is_local=true is the parameterized equivalent (issue #42 follow-up).
    await client.query(`SELECT set_config('app.current_user_id', $1, true)`, [userId])
    const result = await client.query(sql, params)
    await client.query('COMMIT')
    return result
  } catch (err) {
    await client.query('ROLLBACK')
    throw err
  } finally {
    client.release()
  }
}
