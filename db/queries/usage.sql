-- name: UpsertIncrementEvaluations :one
INSERT INTO usage (user_id, month, evaluations_count)
VALUES ($1, $2, 1)
ON CONFLICT (user_id, month) DO UPDATE
  SET evaluations_count = usage.evaluations_count + 1
RETURNING *;

-- name: UpsertIncrementPDFs :one
INSERT INTO usage (user_id, month, pdfs_count)
VALUES ($1, $2, 1)
ON CONFLICT (user_id, month) DO UPDATE
  SET pdfs_count = usage.pdfs_count + 1
RETURNING *;

-- name: GetUsageByUserMonth :one
SELECT * FROM usage
WHERE user_id = $1
  AND month   = $2
LIMIT 1;
