-- name: ListDigestsByUser :many
SELECT * FROM article_digests
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: InsertDigest :one
INSERT INTO article_digests (
  user_id,
  title,
  content_md
) VALUES (
  $1, $2, $3
) RETURNING *;

-- name: DeleteDigest :execrows
DELETE FROM article_digests
WHERE id = $1 AND user_id = $2;
