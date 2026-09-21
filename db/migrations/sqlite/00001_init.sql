-- +goose Up
-- Conventions (kept portable on purpose, see README "Future PostgreSQL migration"):
--   * ids are explicit 64-bit integer primary keys
--   * timestamps are UTC unix seconds stored as integers
--   * no triggers, no generated columns, no database-side business logic

CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    email         TEXT    NOT NULL UNIQUE,
    password_hash TEXT    NOT NULL,
    display_name  TEXT    NOT NULL,
    is_admin      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    BIGINT  NOT NULL,
    updated_at    BIGINT  NOT NULL
);

CREATE TABLE sessions (
    id         INTEGER PRIMARY KEY,
    token_hash TEXT    NOT NULL UNIQUE,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at BIGINT  NOT NULL,
    expires_at BIGINT  NOT NULL
);

CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

CREATE TABLE projects (
    id                      INTEGER PRIMARY KEY,
    name                    TEXT    NOT NULL,
    description             TEXT    NOT NULL DEFAULT '',
    slug                    TEXT    NOT NULL UNIQUE,
    iteration_length_days   INTEGER NOT NULL DEFAULT 7,
    iteration_start_weekday INTEGER NOT NULL DEFAULT 1,
    velocity_window         INTEGER NOT NULL DEFAULT 3,
    created_at              BIGINT  NOT NULL,
    updated_at              BIGINT  NOT NULL
);

CREATE TABLE stories (
    id           INTEGER PRIMARY KEY,
    project_id   INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    title        TEXT    NOT NULL,
    description  TEXT    NOT NULL DEFAULT '',
    type         TEXT    NOT NULL,
    state        TEXT    NOT NULL,
    estimate     INTEGER,
    position     BIGINT  NOT NULL,
    requester_id INTEGER NOT NULL REFERENCES users (id),
    owner_id     INTEGER REFERENCES users (id),
    created_at   BIGINT  NOT NULL,
    updated_at   BIGINT  NOT NULL,
    accepted_at  BIGINT
);

CREATE INDEX stories_project_state_position_idx ON stories (project_id, state, position);
CREATE INDEX stories_project_accepted_at_idx ON stories (project_id, accepted_at);

CREATE TABLE comments (
    id         INTEGER PRIMARY KEY,
    story_id   INTEGER NOT NULL REFERENCES stories (id) ON DELETE CASCADE,
    user_id    INTEGER NOT NULL REFERENCES users (id),
    body       TEXT    NOT NULL,
    created_at BIGINT  NOT NULL,
    updated_at BIGINT  NOT NULL
);

CREATE INDEX comments_story_idx ON comments (story_id, id);

CREATE TABLE labels (
    id         INTEGER PRIMARY KEY,
    project_id INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name       TEXT    NOT NULL,
    UNIQUE (project_id, name)
);

CREATE TABLE story_labels (
    story_id INTEGER NOT NULL REFERENCES stories (id) ON DELETE CASCADE,
    label_id INTEGER NOT NULL REFERENCES labels (id) ON DELETE CASCADE,
    PRIMARY KEY (story_id, label_id)
);

CREATE INDEX story_labels_label_idx ON story_labels (label_id);

-- +goose Down
DROP TABLE story_labels;
DROP TABLE labels;
DROP TABLE comments;
DROP TABLE stories;
DROP TABLE projects;
DROP TABLE sessions;
DROP TABLE users;
