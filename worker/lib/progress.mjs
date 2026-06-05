/**
 * Emit a PostgreSQL NOTIFY event on the `scan_progress` channel.
 *
 * Payload shape:
 *   { event, scan_run_id, ts: <ISO-8601>, data }
 *
 * Consumers (e.g., SSE endpoint in the Go API) listen on `scan_progress`
 * and forward events to connected clients in real-time.
 *
 * @param {import('pg').PoolClient} pgClient - Active pg client
 * @param {string} scanRunId - UUID of the scan run
 * @param {string} event - Event name (e.g., 'scan.started', 'scan.job_found')
 * @param {object} data - Event-specific payload
 */
export async function notify(pgClient, scanRunId, event, data) {
  const payload = JSON.stringify({
    event,
    scan_run_id: scanRunId,
    ts: new Date().toISOString(),
    data,
  })

  await pgClient.query(`SELECT pg_notify('scan_progress', $1)`, [payload])
}
