-- name: CreateProject :one
INSERT INTO projects (name, description, slug, iteration_length_days, iteration_start_weekday,
                      velocity_window, estimate_bugs_and_chores, created_at, updated_at)
VALUES (sqlc.arg(name), sqlc.arg(description), sqlc.arg(slug), sqlc.arg(iteration_length_days),
        sqlc.arg(iteration_start_weekday), sqlc.arg(velocity_window), sqlc.arg(estimate_bugs_and_chores),
        sqlc.arg(now), sqlc.arg(now))
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
    estimate_bugs_and_chores = sqlc.arg(estimate_bugs_and_chores),
    updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id)
RETURNING *;

-- Turning bug and chore points off drops the points they already have, so
-- the board and velocity never see points the project does not allow.
-- name: ClearNonFeatureEstimates :execrows
UPDATE stories
SET estimate = NULL, updated_at = sqlc.arg(now)
WHERE project_id = sqlc.arg(project_id) AND type <> 'feature' AND estimate IS NOT NULL;

-- name: DeleteProject :exec
DELETE FROM projects WHERE id = sqlc.arg(id);

-- name: SetProjectArchived :one
UPDATE projects
SET archived_at = sqlc.narg(archived_at),
    updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id)
RETURNING *;
