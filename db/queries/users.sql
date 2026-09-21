-- name: CreateUser :one
INSERT INTO users (email, password_hash, display_name, is_admin, created_at, updated_at)
VALUES (sqlc.arg(email), sqlc.arg(password_hash), sqlc.arg(display_name), sqlc.arg(is_admin), sqlc.arg(now), sqlc.arg(now))
RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE id = sqlc.arg(id);

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = sqlc.arg(email);

-- name: ListUsers :many
SELECT * FROM users ORDER BY display_name, id;

-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: CreateSession :exec
INSERT INTO sessions (token_hash, user_id, created_at, expires_at)
VALUES (sqlc.arg(token_hash), sqlc.arg(user_id), sqlc.arg(created_at), sqlc.arg(expires_at));

-- name: GetSessionUser :one
SELECT users.*
FROM sessions
JOIN users ON users.id = sessions.user_id
WHERE sessions.token_hash = sqlc.arg(token_hash)
  AND sessions.expires_at > sqlc.arg(now);

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = sqlc.arg(token_hash);

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= sqlc.arg(now);
