-- name: InsertEmailIngestRun :one
INSERT INTO email_ingest_runs (user_id)
VALUES ($1)
RETURNING *;

-- name: GetEmailIngestRunByID :one
SELECT * FROM email_ingest_runs
WHERE id = $1
LIMIT 1;

-- name: UpdateEmailIngestRunStatus :one
UPDATE email_ingest_runs
SET status      = $2,
    finished_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateEmailIngestRunNewJobs :one
UPDATE email_ingest_runs
SET new_jobs = new_jobs + $2
WHERE id = $1
RETURNING *;

-- name: AppendEmailIngestRunError :one
UPDATE email_ingest_runs
SET errors_json = errors_json || $2::jsonb
WHERE id = $1
RETURNING *;
