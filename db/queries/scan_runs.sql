-- name: InsertScanRun :one
INSERT INTO scan_runs (user_id)
VALUES ($1)
RETURNING *;

-- name: UpdateScanRunStatus :one
UPDATE scan_runs
SET status      = $2,
    finished_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateScanRunNewJobs :one
UPDATE scan_runs
SET new_jobs = new_jobs + $2
WHERE id = $1
RETURNING *;

-- name: AppendScanRunError :one
UPDATE scan_runs
SET errors_json = errors_json || $2::jsonb
WHERE id = $1
RETURNING *;

-- name: GetScanRunByID :one
SELECT * FROM scan_runs
WHERE id = $1
LIMIT 1;
