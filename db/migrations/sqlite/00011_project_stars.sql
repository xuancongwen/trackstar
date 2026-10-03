-- +goose Up
-- Projects a user has starred; the projects page lists them first.
CREATE TABLE project_stars (
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    project_id INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    created_at BIGINT  NOT NULL,
    PRIMARY KEY (user_id, project_id)
);

-- +goose Down
DROP TABLE project_stars;
