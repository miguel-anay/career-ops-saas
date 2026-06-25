-- name: ListCompaniesCatalog :many
SELECT * FROM companies_catalog
ORDER BY name ASC;

-- name: GetCompaniesCatalogByID :one
SELECT * FROM companies_catalog
WHERE id = $1
LIMIT 1;
