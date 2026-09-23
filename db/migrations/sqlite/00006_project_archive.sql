-- +goose Up
-- Archived projects are kept but read-only: stories, epics, tasks and
-- comments can no longer change until an owner unarchives the project.
-- NULL means active.
ALTER TABLE projects ADD COLUMN archived_at BIGINT;

-- +goose Down
ALTER TABLE projects DROP COLUMN archived_at;
