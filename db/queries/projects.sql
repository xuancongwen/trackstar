-- name: CreateProject :one
INSERT INTO projects (name, description, slug, iteration_length_days, iteration_start_weekday,
                      velocity_window, created_at, updated_at)
VALUES (sqlc.arg(name), sqlc.arg(description), sqlc.arg(slug), sqlc.arg(iteration_length_days),
        sqlc.arg(iteration_start_weekday), sqlc.arg(velocity_window), sqlc.arg(now), sqlc.arg(now))
RETURNING *;

-- name: GetProject :one
SELECT * FROM projects WHERE id = sqlc.arg(id);

-- name: GetProjectBySlug :one
SELECT * FROM projects WHERE slug = sqlc.arg(slug);

-- name: ListProjects :many
SELECT * FROM projects ORDER BY name, id;

-- name: UpdateProject :one
UPDATE projects
SET name = sqlc.arg(name),
    description = sqlc.arg(description),
    iteration_length_days = sqlc.arg(iteration_length_days),
    iteration_start_weekday = sqlc.arg(iteration_start_weekday),
    velocity_window = sqlc.arg(velocity_window),
    updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteProject :exec
DELETE FROM projects WHERE id = sqlc.arg(id);
