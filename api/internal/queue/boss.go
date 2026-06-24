package queue

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Job represents a pg-boss compatible job to be enqueued.
type Job struct {
	Name string
	Data json.RawMessage
}

// insertJobSQL replicates pg-boss v10.4.2's manager.js insertJobCommand
// (src/plans.js insertJob()) parameter-for-parameter, for the plain
// boss.send(name, data) call shape with no per-job options (no custom
// priority/startAfter/singletonKey/deadLetter/expireIn/keepUntil/retry*).
//
// This is INTENTIONALLY a hand-replicated copy of pg-boss's own SQL
// contract — Go has no pg-boss client library, so this is the only way to
// stay byte-for-byte compatible with what the Node worker's boss.send()
// would have produced for the same call. The JOIN against pgboss.queue is
// pg-boss's own mandatory queue-registration check: if the queue named by
// job.Name was never registered via boss.createQueue() (see
// worker/scripts/install-pgboss.mjs, run by the DB admin), the JOIN yields
// zero rows, the INSERT inserts nothing, and RETURNING id returns zero
// rows. pg-boss's own createJob() treats that as a silent null (see
// manager.js:380-382) — we do NOT replicate that silence; see the explicit
// row-count check below.
//
// Keep this in sync with worker/node_modules/pg-boss/src/plans.js
// insertJob() if pg-boss is ever upgraded. worker/package.json pins pg-boss
// to an EXACT version (10.4.2, not ^10.0.0) specifically so this SQL cannot
// silently drift out from under a minor/patch bump; a version bump must be
// a deliberate, reviewed change to both worker/package.json and this file.
const insertJobSQL = `
	INSERT INTO pgboss.job (
		id,
		name,
		data,
		priority,
		start_after,
		singleton_key,
		singleton_on,
		dead_letter,
		expire_in,
		keep_until,
		retry_limit,
		retry_delay,
		retry_backoff,
		policy
	)
	SELECT
		id,
		j.name,
		data,
		priority,
		start_after,
		singleton_key,
		singleton_on,
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

// Enqueue inserts a job into the real pg-boss v10 partitioned pgboss.job
// table, replicating the SQL contract behind pg-boss's own boss.send(name,
// data) call with no extra options (see insertJobSQL above).
//
// pgboss.* has no RLS policy (it is not a tenant-scoped schema) — pool MUST
// be the raw pgxpool.Pool, never a tenant transaction from
// platform.WithTenantTx.
//
// pg-boss requires the queue to be pre-registered via boss.createQueue()
// (run out-of-band by the DB admin, see worker/scripts/install-pgboss.mjs)
// before any job can be enqueued under that name. If the queue is not
// registered, the INSERT's JOIN against pgboss.queue yields zero rows and
// nothing is inserted — pg-boss's own client treats this as a silent null
// (manager.js:380-382 createJob()). Enqueue does NOT replicate that
// silence: a zero-row RETURNING result is treated as an explicit error.
func Enqueue(ctx context.Context, pool *pgxpool.Pool, job Job) error {
	var id string
	err := pool.QueryRow(ctx, insertJobSQL,
		nil,      // $1 id — let pg-boss generate one (gen_random_uuid())
		job.Name, // $2 name
		job.Data, // $3 data
		nil,      // $4 priority
		nil,      // $5 startAfter
		nil,      // $6 singletonKey
		nil,      // $7 singletonSeconds
		nil,      // $8 singletonOffset
		nil,      // $9 deadLetter
		nil,      // $10 expireIn
		nil,      // $11 expireInDefault
		nil,      // $12 keepUntil
		nil,      // $13 keepUntilDefault
		nil,      // $14 retryLimit
		nil,      // $15 retryLimitDefault
		nil,      // $16 retryDelay
		nil,      // $17 retryDelayDefault
		nil,      // $18 retryBackoff
		nil,      // $19 retryBackoffDefault
	).Scan(&id)
	if err != nil {
		// pgx returns pgx.ErrNoRows when RETURNING produced zero rows — the
		// "queue not registered" silent-failure trap pg-boss itself would
		// swallow. Surface it loudly instead.
		return fmt.Errorf("enqueue job %q: queue not registered or insert rejected: %w", job.Name, err)
	}
	if id == "" {
		return fmt.Errorf("enqueue job %q: insert returned no id", job.Name)
	}

	return nil
}
