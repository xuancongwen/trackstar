-- name: CreateTask :one
INSERT INTO tasks (story_id, description, position, created_at, updated_at)
VALUES (sqlc.arg(story_id), sqlc.arg(description), sqlc.arg(position), sqlc.arg(now), sqlc.arg(now))
RETURNING *;

-- name: GetTask :one
SELECT * FROM tasks WHERE id = sqlc.arg(id);

-- name: UpdateTask :one
UPDATE tasks
SET description = sqlc.arg(description), done = sqlc.arg(done), position = sqlc.arg(position), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteTask :exec
DELETE FROM tasks WHERE id = sqlc.arg(id);

-- name: ListTasks :many
SELECT * FROM tasks WHERE story_id = sqlc.arg(story_id) ORDER BY position, id;

-- name: MaxTaskPosition :one
SELECT COALESCE(MAX(position), 0) FROM tasks WHERE story_id = sqlc.arg(story_id);

-- name: CountTasksByProject :many
SELECT tasks.story_id, COUNT(*) AS total, COUNT(CASE WHEN tasks.done THEN 1 END) AS done
FROM tasks
JOIN stories ON stories.id = tasks.story_id
WHERE stories.project_id = sqlc.arg(project_id)
GROUP BY tasks.story_id;

-- name: ClearStoryBlockers :exec
DELETE FROM story_blockers WHERE story_id = sqlc.arg(story_id);

-- name: AddStoryBlocker :exec
INSERT INTO story_blockers (story_id, blocker_id) VALUES (sqlc.arg(story_id), sqlc.arg(blocker_id));

-- name: ListStoryBlockers :many
SELECT blocker_id FROM story_blockers WHERE story_id = sqlc.arg(story_id) ORDER BY blocker_id;

-- name: ListProjectBlockers :many
SELECT story_blockers.story_id, story_blockers.blocker_id
FROM story_blockers
JOIN stories ON stories.id = story_blockers.story_id
WHERE stories.project_id = sqlc.arg(project_id)
ORDER BY story_blockers.story_id, story_blockers.blocker_id;

-- name: ListSavedFilters :many
SELECT * FROM saved_filters
WHERE user_id = sqlc.arg(user_id) AND project_id = sqlc.arg(project_id)
ORDER BY name;

-- name: CreateSavedFilter :one
INSERT INTO saved_filters (user_id, project_id, name, query, created_at)
VALUES (sqlc.arg(user_id), sqlc.arg(project_id), sqlc.arg(name), sqlc.arg(query), sqlc.arg(now))
RETURNING *;

-- name: DeleteSavedFilter :execrows
DELETE FROM saved_filters WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

-- name: ListProjectMembers :many
SELECT project_members.user_id, project_members.role
FROM project_members
WHERE project_id = sqlc.arg(project_id)
ORDER BY user_id;

-- name: CountProjectMembers :one
SELECT COUNT(*) FROM project_members WHERE project_id = sqlc.arg(project_id);

-- name: GetProjectMember :one
SELECT role FROM project_members WHERE project_id = sqlc.arg(project_id) AND user_id = sqlc.arg(user_id);

-- name: UpsertProjectMember :exec
INSERT INTO project_members (project_id, user_id, role)
VALUES (sqlc.arg(project_id), sqlc.arg(user_id), sqlc.arg(role))
ON CONFLICT (project_id, user_id) DO UPDATE SET role = excluded.role;

-- name: DeleteProjectMember :exec
DELETE FROM project_members WHERE project_id = sqlc.arg(project_id) AND user_id = sqlc.arg(user_id);

-- Projects visible to a user: those with no members at all, or where the user is one.
-- name: ListProjectsForUser :many
SELECT projects.* FROM projects
WHERE NOT EXISTS (SELECT 1 FROM project_members WHERE project_members.project_id = projects.id)
   OR EXISTS (SELECT 1 FROM project_members WHERE project_members.project_id = projects.id AND project_members.user_id = sqlc.arg(user_id))
ORDER BY projects.name, projects.id;
