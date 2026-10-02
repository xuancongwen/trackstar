-- +goose Up
-- A personal preference: projects this user creates start with
-- projects.combine_icebox_backlog on. Existing projects are not touched.
ALTER TABLE users ADD COLUMN default_combine_icebox_backlog BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE users DROP COLUMN default_combine_icebox_backlog;
