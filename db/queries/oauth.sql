-- name: CreateOAuthClient :one
INSERT INTO oauth_clients (client_id, name, redirect_uris, created_at)
VALUES (sqlc.arg(client_id), sqlc.arg(name), sqlc.arg(redirect_uris), sqlc.arg(created_at))
RETURNING *;

-- name: GetOAuthClient :one
SELECT * FROM oauth_clients WHERE client_id = sqlc.arg(client_id);

-- name: TouchOAuthClient :exec
UPDATE oauth_clients SET last_used_at = sqlc.arg(now) WHERE id = sqlc.arg(id);

-- name: DeleteIdleOAuthClients :exec
DELETE FROM oauth_clients
WHERE COALESCE(oauth_clients.last_used_at, oauth_clients.created_at) <= sqlc.arg(before)
  AND NOT EXISTS (SELECT 1 FROM oauth_grants WHERE oauth_grants.client_id = oauth_clients.id);

-- name: CreateOAuthCode :exec
INSERT INTO oauth_codes (code_hash, user_id, client_id, redirect_uri, code_challenge, expires_at)
VALUES (sqlc.arg(code_hash), sqlc.arg(user_id), sqlc.arg(client_id), sqlc.arg(redirect_uri), sqlc.arg(code_challenge), sqlc.arg(expires_at));

-- name: GetOAuthCode :one
SELECT * FROM oauth_codes WHERE code_hash = sqlc.arg(code_hash);

-- name: UseOAuthCode :execrows
UPDATE oauth_codes SET used_at = sqlc.arg(now), grant_id = sqlc.arg(grant_id)
WHERE id = sqlc.arg(id) AND used_at IS NULL;

-- name: DeleteExpiredOAuthCodes :exec
DELETE FROM oauth_codes WHERE expires_at <= sqlc.arg(now);

-- name: CreateOAuthGrant :one
INSERT INTO oauth_grants (user_id, client_id, created_at)
VALUES (sqlc.arg(user_id), sqlc.arg(client_id), sqlc.arg(created_at))
RETURNING *;

-- name: ListOAuthGrants :many
SELECT oauth_grants.id, oauth_grants.created_at, oauth_grants.last_used_at, oauth_clients.name AS client_name
FROM oauth_grants
JOIN oauth_clients ON oauth_clients.id = oauth_grants.client_id
WHERE oauth_grants.user_id = sqlc.arg(user_id)
ORDER BY oauth_grants.created_at DESC, oauth_grants.id DESC;

-- name: TouchOAuthGrant :exec
UPDATE oauth_grants SET last_used_at = sqlc.arg(now) WHERE id = sqlc.arg(id);

-- name: DeleteOAuthGrant :execrows
DELETE FROM oauth_grants WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

-- name: DeleteOAuthGrantByID :exec
DELETE FROM oauth_grants WHERE id = sqlc.arg(id);

-- name: DeleteUserOAuthGrants :exec
DELETE FROM oauth_grants WHERE user_id = sqlc.arg(user_id);

-- name: DeleteEmptyOAuthGrants :exec
DELETE FROM oauth_grants
WHERE NOT EXISTS (SELECT 1 FROM oauth_tokens WHERE oauth_tokens.grant_id = oauth_grants.id);

-- name: CreateOAuthToken :exec
INSERT INTO oauth_tokens (grant_id, kind, token_hash, expires_at)
VALUES (sqlc.arg(grant_id), sqlc.arg(kind), sqlc.arg(token_hash), sqlc.arg(expires_at));

-- name: GetOAuthToken :one
SELECT oauth_tokens.*, oauth_grants.client_id AS grant_client_id
FROM oauth_tokens
JOIN oauth_grants ON oauth_grants.id = oauth_tokens.grant_id
WHERE oauth_tokens.token_hash = sqlc.arg(token_hash);

-- name: GetOAuthAccessTokenUser :one
SELECT oauth_grants.id AS grant_id, oauth_grants.last_used_at, sqlc.embed(users)
FROM oauth_tokens
JOIN oauth_grants ON oauth_grants.id = oauth_tokens.grant_id
JOIN users ON users.id = oauth_grants.user_id
WHERE oauth_tokens.token_hash = sqlc.arg(token_hash)
  AND oauth_tokens.kind = 'access'
  AND oauth_tokens.expires_at > sqlc.arg(now)
  AND users.is_active;

-- name: GetOAuthGrantUser :one
SELECT users.*
FROM oauth_grants
JOIN users ON users.id = oauth_grants.user_id
WHERE oauth_grants.id = sqlc.arg(id);

-- name: UseOAuthToken :execrows
UPDATE oauth_tokens SET used_at = sqlc.arg(now) WHERE id = sqlc.arg(id) AND used_at IS NULL;

-- name: DeleteExpiredOAuthTokens :exec
DELETE FROM oauth_tokens WHERE expires_at <= sqlc.arg(now);
