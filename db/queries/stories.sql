-- name: CreateStory :one
INSERT INTO stories (project_id, title, description, type, state, estimate, position,
                     requester_id, owner_id, created_at, updated_at)
VALUES (sqlc.arg(project_id), sqlc.arg(title), sqlc.arg(description), sqlc.arg(type), sqlc.arg(state),
        sqlc.narg(estimate), sqlc.arg(position), sqlc.arg(requester_id), sqlc.narg(owner_id),
        sqlc.arg(now), sqlc.arg(now))
RETURNING *;

-- name: GetStory :one
SELECT * FROM stories WHERE id = sqlc.arg(id);

-- name: UpdateStory :one
UPDATE stories
SET title = sqlc.arg(title),
    description = sqlc.arg(description),
    type = sqlc.arg(type),
    state = sqlc.arg(state),
    estimate = sqlc.narg(estimate),
    position = sqlc.arg(position),
    requester_id = sqlc.arg(requester_id),
    owner_id = sqlc.narg(owner_id),
    accepted_at = sqlc.narg(accepted_at),
    updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: SetStoryPosition :exec
UPDATE stories SET position = sqlc.arg(position) WHERE id = sqlc.arg(id);

-- name: DeleteStory :exec
DELETE FROM stories WHERE id = sqlc.arg(id);

-- Every story that is still "live": everything not accepted, plus stories
-- accepted at or after the given instant (the start of the current iteration).
-- name: ListActiveStories :many
SELECT * FROM stories
WHERE project_id = sqlc.arg(project_id)
  AND deleted_at IS NULL
  AND (accepted_at IS NULL OR accepted_at >= sqlc.arg(accepted_since))
ORDER BY position, id;

-- name: ListAcceptedStories :many
SELECT * FROM stories
WHERE project_id = sqlc.arg(project_id)
  AND deleted_at IS NULL
  AND accepted_at IS NOT NULL
  AND accepted_at >= sqlc.arg(accepted_from)
  AND accepted_at < sqlc.arg(accepted_before)
ORDER BY accepted_at, id;

-- Ordered ids/positions for one ordering scope. The caller passes the states
-- that make up the section (see story.Section).
-- name: ListSectionPositions :many
SELECT id, position FROM stories
WHERE project_id = sqlc.arg(project_id)
  AND deleted_at IS NULL
  AND state IN (sqlc.slice(states))
ORDER BY position, id;

-- The pattern ('%term%', lower-cased) is built in Go; LOWER() keeps behaviour
-- identical between SQLite and PostgreSQL. sqlc's SQLite parser has no ESCAPE
-- support, so % and _ typed by a user simply act as wildcards.
-- name: SearchStories :many
SELECT * FROM stories
WHERE project_id = sqlc.arg(project_id)
  AND deleted_at IS NULL
  AND LOWER(title || ' ' || description) LIKE sqlc.arg(pattern)
ORDER BY position, id;

-- name: CreateComment :one
INSERT INTO comments (story_id, user_id, body, created_at, updated_at)
VALUES (sqlc.arg(story_id), sqlc.arg(user_id), sqlc.arg(body), sqlc.arg(now), sqlc.arg(now))
RETURNING *;

-- name: ListComments :many
SELECT * FROM comments WHERE story_id = sqlc.arg(story_id) ORDER BY id;

-- name: GetComment :one
SELECT * FROM comments WHERE id = sqlc.arg(id);

-- name: DeleteComment :exec
DELETE FROM comments WHERE id = sqlc.arg(id);

-- name: CountCommentsByProject :many
SELECT comments.story_id, COUNT(*) AS total
FROM comments
JOIN stories ON stories.id = comments.story_id
WHERE stories.project_id = sqlc.arg(project_id)
GROUP BY comments.story_id;

-- name: ListDeletedStories :many
SELECT * FROM stories
WHERE project_id = sqlc.arg(project_id) AND deleted_at IS NOT NULL
ORDER BY deleted_at DESC, id DESC;

-- name: SetStoryDeleted :exec
UPDATE stories SET deleted_at = sqlc.narg(deleted_at), updated_at = sqlc.arg(now) WHERE id = sqlc.arg(id);

-- name: PurgeDeletedStories :execrows
DELETE FROM stories WHERE deleted_at IS NOT NULL AND deleted_at < sqlc.arg(before);

-- Live (not deleted) story counts per project and state, for system info.
-- name: CountStoriesByState :many
SELECT project_id, state, COUNT(*) AS total
FROM stories
WHERE deleted_at IS NULL
GROUP BY project_id, state;

-- name: CreateActivity :exec
INSERT INTO activity (story_id, user_id, kind, old_value, new_value, created_at)
VALUES (sqlc.arg(story_id), sqlc.arg(user_id), sqlc.arg(kind), sqlc.arg(old_value), sqlc.arg(new_value), sqlc.arg(now));

-- name: ListActivity :many
SELECT * FROM activity WHERE story_id = sqlc.arg(story_id) ORDER BY id;

-- Per project: stories being worked on, stories delivered and waiting for
-- acceptance, and when a story last changed (trashed stories count for the
-- time only).
-- name: ProjectStoryStats :many
SELECT project_id,
       CAST(COALESCE(SUM(CASE WHEN deleted_at IS NULL AND state IN ('started', 'finished', 'rejected') THEN 1 ELSE 0 END), 0) AS INTEGER) AS in_progress,
       CAST(COALESCE(SUM(CASE WHEN deleted_at IS NULL AND state = 'delivered' THEN 1 ELSE 0 END), 0) AS INTEGER) AS to_accept,
       CAST(MAX(updated_at) AS INTEGER) AS last_changed_at
FROM stories
WHERE project_id IN (sqlc.slice(project_ids))
GROUP BY project_id;

-- A comment does not touch its story's updated_at.
-- name: ProjectLastComment :many
SELECT stories.project_id, CAST(MAX(comments.created_at) AS INTEGER) AS last_comment_at
FROM comments
JOIN stories ON stories.id = comments.story_id
WHERE stories.project_id IN (sqlc.slice(project_ids))
GROUP BY stories.project_id;

-- Newest first across projects. Renames and type changes are edits, not news.
-- The limit (story.FeedSize) is a literal here and in RecentComments: sqlc
-- numbers a parameter that follows a slice as if the slice were one value,
-- so a bound limit would read one of the project ids instead.
-- name: RecentActivity :many
SELECT activity.id, activity.kind, activity.old_value, activity.new_value, activity.created_at, activity.user_id,
       stories.id AS story_id, stories.title AS story_title, stories.project_id
FROM activity
JOIN stories ON stories.id = activity.story_id
WHERE stories.project_id IN (sqlc.slice(project_ids))
  AND activity.kind NOT IN ('title', 'type')
ORDER BY activity.id DESC
LIMIT 60;

-- name: RecentComments :many
SELECT comments.id, comments.body, comments.created_at, comments.user_id,
       stories.id AS story_id, stories.title AS story_title, stories.project_id
FROM comments
JOIN stories ON stories.id = comments.story_id
WHERE stories.project_id IN (sqlc.slice(project_ids))
  AND stories.deleted_at IS NULL
ORDER BY comments.id DESC
LIMIT 60;
