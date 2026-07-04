-- name: GetUserByGoogleID :one
SELECT * FROM users
WHERE google_id = $1
LIMIT 1;

-- name: GetUserByID :one
SELECT * FROM users
WHERE id = $1
LIMIT 1;

-- name: UpdateUserPlan :one
UPDATE users
SET plan = $2
WHERE id = $1
RETURNING *;

-- name: UpdateUserCVMarkdown :one
UPDATE users
SET cv_markdown = $2
WHERE id = $1
RETURNING *;

-- name: UpdateUserProfileJSON :one
UPDATE users
SET profile_json = $2
WHERE id = $1
RETURNING *;

-- name: UpdateUserGoogleRefreshToken :one
UPDATE users
SET google_refresh_token = $2
WHERE id = $1
RETURNING *;
