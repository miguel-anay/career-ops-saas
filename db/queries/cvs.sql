-- name: ListCVsByUser :many
SELECT * FROM cvs
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: GetMasterCVByUser :one
SELECT * FROM cvs
WHERE user_id  = $1
  AND is_master = true
LIMIT 1;

-- name: InsertCV :one
INSERT INTO cvs (
  user_id,
  title,
  content_md,
  is_master
) VALUES (
  $1, $2, $3, $4
) RETURNING *;

-- name: UpdateCV :one
UPDATE cvs
SET title      = $2,
    content_md = $3
WHERE id = $1
RETURNING *;

-- name: SetMasterCV :exec
-- Clear the current master then set the new one (two statements — call in a tx)
UPDATE cvs
SET is_master = (id = $1)
WHERE user_id = $2;
