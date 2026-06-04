-- name: GetApplicationByJobID :one
SELECT * FROM applications
WHERE job_id = $1
LIMIT 1;

-- name: InsertApplication :one
INSERT INTO applications (
  user_id,
  job_id,
  score,
  status
) VALUES (
  $1, $2, $3, $4
) RETURNING *;

-- name: UpdateApplicationStatus :one
UPDATE applications
SET status     = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateApplicationNotes :one
UPDATE applications
SET notes      = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateApplicationPDFPath :one
UPDATE applications
SET pdf_path   = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListApplicationsByUser :many
SELECT * FROM applications
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2
OFFSET $3;
