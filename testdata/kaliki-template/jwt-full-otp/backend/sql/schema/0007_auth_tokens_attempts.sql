-- +goose Up
-- OTP codes are short (6 digits) and brute-forceable; cap attempts per credential
-- row so a leaked/guessed verification_token can't be hammered indefinitely.
-- Deliberately OTP-only: token-link mode uses high-entropy tokens, no cap needed.
ALTER TABLE auth_tokens ADD COLUMN attempts INT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE auth_tokens DROP COLUMN attempts;
