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

-- name: UpdateUserGoogleRefreshToken :one
UPDATE users
SET google_refresh_token = $2
WHERE id = $1
RETURNING *;

-- name: GetUserProfile :one
SELECT cv_markdown, profile_json, profile_overrides
FROM users WHERE id = $1 LIMIT 1;

-- name: SetProfileOverrideKey :one
UPDATE users
SET profile_overrides = profile_overrides || jsonb_build_object($2::text, $3::jsonb)
WHERE id = $1
RETURNING profile_overrides;

-- name: DropProfileOverrideKey :one
UPDATE users
SET profile_overrides = profile_overrides - $2::text
WHERE id = $1
RETURNING profile_overrides;
