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

-- name: DeleteUnusedLabels :exec
DELETE FROM labels
WHERE labels.project_id = sqlc.arg(project_id)
  AND NOT EXISTS (SELECT 1 FROM story_labels WHERE story_labels.label_id = labels.id);
