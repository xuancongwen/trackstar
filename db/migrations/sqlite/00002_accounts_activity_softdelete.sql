-- +goose Up
ALTER TABLE users ADD COLUMN is_active BOOLEAN NOT NULL DEFAULT TRUE;

-- Soft delete: rows stay for 30 days so a mistake can be undone.
ALTER TABLE stories ADD COLUMN deleted_at BIGINT;
CREATE INDEX stories_project_deleted_idx ON stories (project_id, deleted_at);

-- Who changed what on a story. Written by the story service in the same
-- transaction as the change itself; comments stay in their own table.
CREATE TABLE activity (
    id         INTEGER PRIMARY KEY,
    story_id   INTEGER NOT NULL REFERENCES stories (id) ON DELETE CASCADE,
    user_id    INTEGER NOT NULL REFERENCES users (id),
    kind       TEXT    NOT NULL,
    old_value  TEXT    NOT NULL DEFAULT '',
    new_value  TEXT    NOT NULL DEFAULT '',
    created_at BIGINT  NOT NULL
);

CREATE INDEX activity_story_idx ON activity (story_id, id);

-- +goose Down
DROP TABLE activity;
DROP INDEX stories_project_deleted_idx;
ALTER TABLE stories DROP COLUMN deleted_at;
ALTER TABLE users DROP COLUMN is_active;
