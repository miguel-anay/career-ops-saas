-- name: InsertProfileEdit :one
INSERT INTO profile_edits (user_id, field_path, old_value, new_value, source, status)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListProfileEditsByUser :many
SELECT * FROM profile_edits
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: GetProfileEdit :one
SELECT * FROM profile_edits
WHERE id = $1
LIMIT 1;

-- name: MarkProfileEditUndone :one
UPDATE profile_edits
SET status = 'undone', resolved_at = NOW()
WHERE id = $1
RETURNING *;
