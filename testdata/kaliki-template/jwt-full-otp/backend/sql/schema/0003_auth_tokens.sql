-- +goose Up
CREATE TABLE auth_tokens (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       TEXT NOT NULL, -- 'verify' | 'password_reset'
    token_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- OTP codes are short and shared across users, so token_hash is not unique;
-- look up the latest live code per user/kind instead.
CREATE INDEX idx_auth_tokens_user ON auth_tokens (user_id, kind, created_at DESC);

-- +goose Down
DROP TABLE auth_tokens;
