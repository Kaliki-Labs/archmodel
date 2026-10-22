-- name: CreateUser :one
INSERT INTO users (email, password_hash, name, role)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: CreateRefreshToken :one
INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetRefreshToken :one
SELECT * FROM refresh_tokens
WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now();

-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens SET revoked_at = now() WHERE id = $1;

-- name: VerifyUser :exec
UPDATE users SET verified = true, updated_at = now() WHERE id = $1;

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1;

-- name: CreateAuthToken :one
INSERT INTO auth_tokens (user_id, kind, token_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetAuthToken :one
SELECT * FROM auth_tokens
WHERE token_hash = $1 AND kind = $2 AND used_at IS NULL AND expires_at > now();

-- name: MarkAuthTokenUsed :exec
UPDATE auth_tokens SET used_at = now() WHERE id = $1;
