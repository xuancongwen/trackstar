-- +goose Up

-- Epics are labels with a description and a flag; progress is computed.
ALTER TABLE labels ADD COLUMN is_epic BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE labels ADD COLUMN description TEXT NOT NULL DEFAULT '';

CREATE TABLE tasks (
    id          INTEGER PRIMARY KEY,
    story_id    INTEGER NOT NULL REFERENCES stories (id) ON DELETE CASCADE,
    description TEXT    NOT NULL,
    done        BOOLEAN NOT NULL DEFAULT FALSE,
    position    BIGINT  NOT NULL,
    created_at  BIGINT  NOT NULL,
    updated_at  BIGINT  NOT NULL
);

CREATE INDEX tasks_story_idx ON tasks (story_id, position, id);

-- story_id is blocked by blocker_id. Informational only.
CREATE TABLE story_blockers (
    story_id   INTEGER NOT NULL REFERENCES stories (id) ON DELETE CASCADE,
    blocker_id INTEGER NOT NULL REFERENCES stories (id) ON DELETE CASCADE,
    PRIMARY KEY (story_id, blocker_id)
);

CREATE INDEX story_blockers_blocker_idx ON story_blockers (blocker_id);

CREATE TABLE saved_filters (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    project_id INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name       TEXT    NOT NULL,
    query      TEXT    NOT NULL,
    created_at BIGINT  NOT NULL,
    UNIQUE (user_id, project_id, name)
);

-- A project with no members is open to every signed-in user; with members,
-- only they (and administrators) can see it. role: member | viewer.
CREATE TABLE project_members (
    project_id INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role       TEXT    NOT NULL,
    PRIMARY KEY (project_id, user_id)
);

CREATE INDEX project_members_user_idx ON project_members (user_id);

-- +goose Down
DROP TABLE project_members;
DROP TABLE saved_filters;
DROP TABLE story_blockers;
DROP TABLE tasks;
ALTER TABLE labels DROP COLUMN description;
ALTER TABLE labels DROP COLUMN is_epic;
