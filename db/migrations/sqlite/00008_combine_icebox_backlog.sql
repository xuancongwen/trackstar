-- +goose Up
-- A view setting: the board shows the icebox at the bottom of the backlog
-- panel instead of in a panel of its own. Stories keep their sections.
ALTER TABLE projects ADD COLUMN combine_icebox_backlog BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE projects DROP COLUMN combine_icebox_backlog;
