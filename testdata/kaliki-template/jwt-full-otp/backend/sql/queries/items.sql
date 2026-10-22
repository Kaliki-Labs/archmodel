-- name: ListItems :many
SELECT id, name, created_at FROM items ORDER BY created_at DESC;

-- name: CreateItem :one
INSERT INTO items (name) VALUES ($1) RETURNING id, name, created_at;

-- name: GetItem :one
SELECT id, name, created_at FROM items WHERE id = $1;
