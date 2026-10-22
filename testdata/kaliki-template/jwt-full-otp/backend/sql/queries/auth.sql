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

-- name: IncrementAuthTokenAttempts :one
-- Atomically picks the latest live (unused, unexpired) credential for the
-- user/kind with attempts remaining and increments its attempts counter in the
-- same statement. Zero rows back means "no eligible credential" — expired,
-- already used, and exhausted-attempts are deliberately indistinguishable from
-- the caller's perspective (all surface as "invalid or expired").
UPDATE auth_tokens
SET attempts = attempts + 1
WHERE id = (
    SELECT t.id FROM auth_tokens t
    WHERE t.user_id = $1 AND t.kind = $2 AND t.used_at IS NULL AND t.expires_at > now() AND t.attempts < 5
    ORDER BY t.created_at DESC
    LIMIT 1
)
RETURNING *;

-- name: MarkAuthTokenUsed :exec
UPDATE auth_tokens SET used_at = now() WHERE id = $1;
