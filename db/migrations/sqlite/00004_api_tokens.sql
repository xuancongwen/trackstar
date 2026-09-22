-- +goose Up
-- Personal API tokens: long-lived bearer credentials for scripts and MCP
-- clients. Like sessions, only an HMAC of the token is stored, so a leaked
-- database does not leak usable credentials. A token acts as its owner.
CREATE TABLE api_tokens (
    id           INTEGER PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    token_hash   TEXT    NOT NULL UNIQUE,
    created_at   BIGINT  NOT NULL,
    last_used_at BIGINT,
    expires_at   BIGINT
);

CREATE INDEX api_tokens_user_idx ON api_tokens (user_id);

-- +goose Down
DROP TABLE api_tokens;
