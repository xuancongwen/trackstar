-- +goose Up
-- OAuth 2.1 for MCP clients that are given only the server URL. Trackstar is
-- its own authorization server: a client registers itself, the user approves
-- it in the browser, and the client then holds a short-lived access token and
-- a rotating refresh token. As with sessions and API tokens, only HMACs of
-- codes and tokens are stored.

-- Clients are public (no secret; PKCE is their proof) and self-registered, so
-- the name is whatever the client claimed. redirect_uris is a JSON array.
CREATE TABLE oauth_clients (
    id            INTEGER PRIMARY KEY,
    client_id     TEXT    NOT NULL UNIQUE,
    name          TEXT    NOT NULL,
    redirect_uris TEXT    NOT NULL,
    created_at    BIGINT  NOT NULL,
    last_used_at  BIGINT
);

-- One row per approval: "this client may act as this user". Deleting it
-- deletes its tokens, which is how an app is disconnected.
CREATE TABLE oauth_grants (
    id           INTEGER PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    client_id    INTEGER NOT NULL REFERENCES oauth_clients (id) ON DELETE CASCADE,
    created_at   BIGINT  NOT NULL,
    last_used_at BIGINT
);

CREATE INDEX oauth_grants_user_idx ON oauth_grants (user_id);
CREATE INDEX oauth_grants_client_idx ON oauth_grants (client_id);

-- Authorization codes. A redeemed code is kept until it expires, pointing at
-- the grant it produced, so a second presentation can revoke that grant.
CREATE TABLE oauth_codes (
    id             INTEGER PRIMARY KEY,
    code_hash      TEXT    NOT NULL UNIQUE,
    user_id        INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    client_id      INTEGER NOT NULL REFERENCES oauth_clients (id) ON DELETE CASCADE,
    redirect_uri   TEXT    NOT NULL,
    code_challenge TEXT    NOT NULL,
    expires_at     BIGINT  NOT NULL,
    used_at        BIGINT,
    grant_id       INTEGER REFERENCES oauth_grants (id) ON DELETE SET NULL
);

CREATE INDEX oauth_codes_expires_at_idx ON oauth_codes (expires_at);

-- Access and refresh tokens of a grant. A refresh token that has been
-- rotated keeps its row (used_at set) until it expires, so reuse is
-- recognised as a leak and revokes the grant.
CREATE TABLE oauth_tokens (
    id         INTEGER PRIMARY KEY,
    grant_id   INTEGER NOT NULL REFERENCES oauth_grants (id) ON DELETE CASCADE,
    kind       TEXT    NOT NULL CHECK (kind IN ('access', 'refresh')),
    token_hash TEXT    NOT NULL UNIQUE,
    expires_at BIGINT  NOT NULL,
    used_at    BIGINT
);

CREATE INDEX oauth_tokens_grant_idx ON oauth_tokens (grant_id);
CREATE INDEX oauth_tokens_expires_at_idx ON oauth_tokens (expires_at);

-- +goose Down
DROP TABLE oauth_tokens;
DROP TABLE oauth_codes;
DROP TABLE oauth_grants;
DROP TABLE oauth_clients;
