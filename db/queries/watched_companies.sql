-- name: ListWatchedCompaniesByUser :many
SELECT * FROM watched_companies
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: GetWatchedCompanyByID :one
SELECT * FROM watched_companies
WHERE id = $1
LIMIT 1;

-- name: InsertWatchedCompany :one
INSERT INTO watched_companies (
  user_id,
  name,
  careers_url,
  provider_id,
  ats_api_url,
  enabled
) VALUES (
  $1, $2, $3, $4, $5, $6
) RETURNING *;

-- name: DeleteWatchedCompany :exec
DELETE FROM watched_companies
WHERE id = $1;

-- name: ListEnabledWatchedCompaniesByUser :many
SELECT * FROM watched_companies
WHERE user_id = $1
  AND enabled = true
ORDER BY created_at DESC;
