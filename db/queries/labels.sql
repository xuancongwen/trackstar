-- name: GetLabelByName :one
SELECT * FROM labels WHERE project_id = sqlc.arg(project_id) AND name = sqlc.arg(name);

-- name: CreateLabel :one
INSERT INTO labels (project_id, name) VALUES (sqlc.arg(project_id), sqlc.arg(name))
RETURNING *;

-- name: ListLabels :many
SELECT * FROM labels WHERE project_id = sqlc.arg(project_id) ORDER BY name;

-- name: AddStoryLabel :exec
INSERT INTO story_labels (story_id, label_id) VALUES (sqlc.arg(story_id), sqlc.arg(label_id));

-- name: ClearStoryLabels :exec
DELETE FROM story_labels WHERE story_id = sqlc.arg(story_id);

-- name: ListStoryLabels :many
SELECT labels.name
FROM story_labels
JOIN labels ON labels.id = story_labels.label_id
WHERE story_labels.story_id = sqlc.arg(story_id)
ORDER BY labels.name;

-- name: ListProjectStoryLabels :many
SELECT story_labels.story_id, labels.name
FROM story_labels
JOIN labels ON labels.id = story_labels.label_id
WHERE labels.project_id = sqlc.arg(project_id)
ORDER BY labels.name;

-- Plain labels disappear with their last story; epics are kept until demoted.
-- name: DeleteUnusedLabels :exec
DELETE FROM labels
WHERE labels.project_id = sqlc.arg(project_id)
  AND NOT labels.is_epic
  AND NOT EXISTS (SELECT 1 FROM story_labels WHERE story_labels.label_id = labels.id);

-- name: GetLabel :one
SELECT * FROM labels WHERE id = sqlc.arg(id);

-- name: UpdateLabel :one
UPDATE labels
SET name = sqlc.arg(name), description = sqlc.arg(description), is_epic = sqlc.arg(is_epic)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: CreateEpic :one
INSERT INTO labels (project_id, name, description, is_epic)
VALUES (sqlc.arg(project_id), sqlc.arg(name), sqlc.arg(description), TRUE)
RETURNING *;

-- Live stories per label with what progress needs.
-- name: ListLabelStoryStats :many
SELECT story_labels.label_id, stories.type, stories.state, stories.estimate
FROM story_labels
JOIN stories ON stories.id = story_labels.story_id
JOIN labels ON labels.id = story_labels.label_id
WHERE labels.project_id = sqlc.arg(project_id) AND stories.deleted_at IS NULL;
