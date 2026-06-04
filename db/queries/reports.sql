-- name: GetReportByApplicationID :one
SELECT * FROM reports
WHERE application_id = $1
LIMIT 1;

-- name: InsertReport :one
INSERT INTO reports (
  user_id,
  application_id,
  content_md,
  blocks_json
) VALUES (
  $1, $2, $3, $4
) RETURNING *;
