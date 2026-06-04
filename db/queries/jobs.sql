-- name: ListJobsByUser :many
SELECT * FROM jobs
WHERE user_id = $1
ORDER BY received_at DESC NULLS LAST, created_at DESC
LIMIT $2
OFFSET $3;

-- name: GetJobByID :one
SELECT * FROM jobs
WHERE id = $1
LIMIT 1;

-- name: InsertJob :one
INSERT INTO jobs (
  user_id,
  title,
  company,
  url,
  platform,
  status,
  scraped_content,
  received_at
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8
) RETURNING *;

-- name: UpdateJobStatus :one
UPDATE jobs
SET status = $2
WHERE id = $1
RETURNING *;

-- name: UpdateJobEvaluationJSON :one
UPDATE jobs
SET evaluation_json = $2,
    status          = 'evaluated'
WHERE id = $1
RETURNING *;

-- name: UpsertJobByURL :one
INSERT INTO jobs (
  user_id,
  title,
  company,
  url,
  platform,
  status,
  scraped_content,
  received_at
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8
) ON CONFLICT (user_id, url) DO UPDATE
  SET title           = EXCLUDED.title,
      company         = EXCLUDED.company,
      platform        = EXCLUDED.platform,
      scraped_content = EXCLUDED.scraped_content,
      received_at     = EXCLUDED.received_at
RETURNING *;
