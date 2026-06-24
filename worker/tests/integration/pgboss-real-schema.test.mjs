// Primary worker-side acceptance test for pgboss-queue-unification.
//
// Proves the REAL pg-boss v10 schema dequeue path works: provisions the
// schema + registers a queue (the same admin-side operations
// worker/scripts/install-pgboss.mjs performs in production), inserts a job
// the SAME way the Go API's queue.Enqueue does (a raw SQL INSERT matching
// pg-boss's own insertJob contract — see api/internal/queue/boss.go), then
// dequeues it via the real pg-boss client's fetch() and asserts job.data
// round-trips exactly.
//
// This is the worker-side half of the cross-service contract test: the Go
// test (api/internal/queue/boss_test.go TestEnqueue_RealV10Schema_Integration)
// proves Go's INSERT lands a readable row; this test proves a real pg-boss
// client can actually consume that row, closing the loop the original
// incident never had (Go and Node were never integration-tested together).
//
// Skips cleanly when TEST_DATABASE_URL is unset. To run against a live DB:
//
//   TEST_DATABASE_URL="postgres://app_user:app_pw@localhost:5432/careerops?sslmode=disable" \
//     TEST_ADMIN_DATABASE_URL="postgres://careerops:careerops@localhost:5432/careerops?sslmode=disable" \
//     npx vitest run tests/integration/pgboss-real-schema.test.mjs
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import pg from 'pg'
import PgBoss from 'pg-boss'

const TEST_DATABASE_URL = process.env.TEST_DATABASE_URL
const TEST_ADMIN_DATABASE_URL =
  process.env.TEST_ADMIN_DATABASE_URL ||
  (TEST_DATABASE_URL && TEST_DATABASE_URL.replace('app_user:app_pw', 'careerops:careerops'))

const QUEUE_NAME = 'pgboss-acceptance-worker-dequeue'

// Replicates the EXACT SQL contract api/internal/queue/boss.go's Enqueue
// uses (pg-boss v10.4.2's insertJob, plain send(name,data) shape, no extra
// options) — this is deliberately a copy, not a call into Go, to prove the
// worker-side dequeue path is compatible with what the Go INSERT actually
// produces, independent of language.
const GO_STYLE_INSERT_JOB_SQL = `
  INSERT INTO pgboss.job (
    id, name, data, priority, start_after, singleton_key, singleton_on,
    dead_letter, expire_in, keep_until, retry_limit, retry_delay,
    retry_backoff, policy
  )
  SELECT
    id, j.name, data, priority, start_after, singleton_key, singleton_on,
    COALESCE(j.dead_letter, q.dead_letter) as dead_letter,
    CASE
      WHEN expire_in IS NOT NULL THEN expire_in::numeric * interval '1s'
      WHEN q.expire_seconds IS NOT NULL THEN q.expire_seconds * interval '1s'
      WHEN expire_in_default IS NOT NULL THEN expire_in_default::numeric * interval '1s'
      ELSE interval '15 minutes'
      END as expire_in,
    CASE
      WHEN right(keep_until, 1) = 'Z' THEN CAST(keep_until as timestamp with time zone)
      ELSE start_after + CAST(COALESCE(keep_until, (q.retention_minutes * 60)::text, keep_until_default, '14 days') as interval)
      END as keep_until,
    COALESCE(j.retry_limit, q.retry_limit, retry_limit_default, 2) as retry_limit,
    CASE
      WHEN COALESCE(j.retry_backoff, q.retry_backoff, retry_backoff_default, false)
      THEN GREATEST(COALESCE(j.retry_delay, q.retry_delay, retry_delay_default), 1)
      ELSE COALESCE(j.retry_delay, q.retry_delay, retry_delay_default, 0)
      END as retry_delay,
    COALESCE(j.retry_backoff, q.retry_backoff, retry_backoff_default, false) as retry_backoff,
    q.policy
  FROM
    ( SELECT
        COALESCE($1::uuid, gen_random_uuid()) as id,
        $2 as name,
        $3::jsonb as data,
        COALESCE($4::int, 0) as priority,
        CASE
          WHEN right($5, 1) = 'Z' THEN CAST($5 as timestamp with time zone)
          ELSE now() + CAST(COALESCE($5,'0') as interval)
          END as start_after,
        $6 as singleton_key,
        CASE
          WHEN $7::integer IS NOT NULL THEN 'epoch'::timestamp + '1 second'::interval * ($7 * floor((date_part('epoch', now()) + $8) / $7))
          ELSE NULL
          END as singleton_on,
        $9 as dead_letter,
        $10 as expire_in,
        $11 as expire_in_default,
        $12 as keep_until,
        $13 as keep_until_default,
        $14::int as retry_limit,
        $15::int as retry_limit_default,
        $16::int as retry_delay,
        $17::int as retry_delay_default,
        $18::bool as retry_backoff,
        $19::bool as retry_backoff_default
    ) j JOIN pgboss.queue q ON j.name = q.name
  ON CONFLICT DO NOTHING
  RETURNING id
`

const describeIfDb = TEST_DATABASE_URL ? describe : describe.skip

describeIfDb('pg-boss real v10 schema: Go-style enqueue -> worker dequeue', () => {
  // adminBoss: provisions the schema + registers the queue. This mirrors the
  // ADMIN-only operations worker/scripts/install-pgboss.mjs performs (app_user
  // has no CREATE and must never register queues — see db/pgboss_grants.sql).
  /** @type {PgBoss} */
  let adminBoss
  // appBoss: the RUNTIME role (app_user), with migrate:false exactly like
  // worker/lib/queue.mjs. The enqueue (raw INSERT) and dequeue (fetch/complete)
  // run as app_user so this test proves the runtime grants in db/pgboss_grants.sql
  // are actually sufficient — that was the original incident's failure mode
  // (the worker could never even connect as app_user).
  /** @type {PgBoss} */
  let appBoss

  beforeAll(async () => {
    adminBoss = new PgBoss({ connectionString: TEST_ADMIN_DATABASE_URL, schema: 'pgboss', migrate: true })
    await adminBoss.start()
    await adminBoss.createQueue(QUEUE_NAME)

    appBoss = new PgBoss({ connectionString: TEST_DATABASE_URL, schema: 'pgboss', migrate: false })
    await appBoss.start()
  })

  afterAll(async () => {
    if (appBoss) await appBoss.stop({ graceful: false })
    if (adminBoss) await adminBoss.stop({ graceful: false })
  })

  it('dequeues a job inserted via the Go-style raw SQL contract (as app_user), with data round-tripped exactly', async () => {
    const payload = { acceptance: 'go-style-insert-worker-dequeue', n: 42 }

    // Insert as app_user (TEST_DATABASE_URL) — the same role api/internal/queue
    // /boss.go's Enqueue runs as. Proves app_user's INSERT grant is sufficient.
    const client = new pg.Client({ connectionString: TEST_DATABASE_URL })
    await client.connect()
    let insertedId
    try {
      const { rows } = await client.query(GO_STYLE_INSERT_JOB_SQL, [
        null, // id
        QUEUE_NAME, // name
        JSON.stringify(payload), // data
        null, null, null, null, null, null, null, null, null, null, null, null, null, null, null, null,
      ])
      expect(rows).toHaveLength(1)
      insertedId = rows[0].id
    } finally {
      await client.end()
    }

    expect(insertedId).toBeTruthy()

    // Dequeue as app_user (the worker's runtime role), not admin.
    const [job] = await appBoss.fetch(QUEUE_NAME)

    expect(job).toBeDefined()
    expect(job.id).toBe(insertedId)
    expect(job.data).toEqual(payload)

    await appBoss.complete(QUEUE_NAME, job.id)
  })

  it('a job enqueued for an UNREGISTERED queue name never becomes fetchable (mirrors Go-side silent-failure trap)', async () => {
    const client = new pg.Client({ connectionString: TEST_DATABASE_URL })
    await client.connect()
    let rowCount
    try {
      const { rows } = await client.query(GO_STYLE_INSERT_JOB_SQL, [
        null,
        'pgboss-acceptance-never-registered',
        JSON.stringify({}),
        null, null, null, null, null, null, null, null, null, null, null, null, null, null, null, null,
      ])
      rowCount = rows.length
    } finally {
      await client.end()
    }

    // This is pg-boss's own documented silent-failure behavior: the JOIN
    // against pgboss.queue yields zero rows for an unregistered name, so
    // the INSERT inserts nothing and RETURNING produces zero rows. The Go
    // side (api/internal/queue/boss.go Enqueue) treats this as an explicit
    // error instead of swallowing it — this assertion documents WHY that
    // defense is necessary from the worker's perspective: there is no row
    // to ever fetch.
    expect(rowCount).toBe(0)
  })
})
