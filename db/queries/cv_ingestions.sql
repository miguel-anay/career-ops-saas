-- name: InsertCVIngestion :one
INSERT INTO cv_ingestions (user_id) VALUES ($1) RETURNING *;

-- name: GetCVIngestion :one
SELECT * FROM cv_ingestions WHERE id = $1 LIMIT 1;

-- name: UpdateCVIngestionStatus :one
UPDATE cv_ingestions SET status = $2, finished_at = now() WHERE id = $1 RETURNING *;
