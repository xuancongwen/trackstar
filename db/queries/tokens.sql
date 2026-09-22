-- name: CreateAPIToken :one
INSERT INTO api_tokens (user_id, name, token_hash, created_at, expires_at)
VALUES (sqlc.arg(user_id), sqlc.arg(name), sqlc.arg(token_hash), sqlc.arg(created_at), sqlc.narg(expires_at))
RETURNING *;

-- name: ListAPITokens :many
SELECT * FROM api_tokens WHERE user_id = sqlc.arg(user_id) ORDER BY created_at DESC, id DESC;

-- name: GetAPITokenUser :one
SELECT api_tokens.id AS token_id, api_tokens.last_used_at, sqlc.embed(users)
FROM api_tokens
JOIN users ON users.id = api_tokens.user_id
WHERE api_tokens.token_hash = sqlc.arg(token_hash)
  AND (api_tokens.expires_at IS NULL OR api_tokens.expires_at > sqlc.arg(now))
  AND users.is_active;

-- name: TouchAPIToken :exec
UPDATE api_tokens SET last_used_at = sqlc.arg(now) WHERE id = sqlc.arg(id);

-- name: DeleteAPIToken :execrows
DELETE FROM api_tokens WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

-- name: DeleteUserAPITokens :exec
DELETE FROM api_tokens WHERE user_id = sqlc.arg(user_id);
