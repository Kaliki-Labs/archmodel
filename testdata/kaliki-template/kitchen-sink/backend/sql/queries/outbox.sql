-- name: ListUnpublishedOutbox :many
SELECT * FROM outbox
WHERE published_at IS NULL
ORDER BY created_at
LIMIT $1;

-- name: MarkOutboxPublished :exec
UPDATE outbox SET published_at = now() WHERE id = $1;

-- name: EnqueueOutbox :one
INSERT INTO outbox (topic, key, payload)
VALUES ($1, $2, $3)
RETURNING *;
