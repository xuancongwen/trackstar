-- +goose Up
-- Bugs and chores carry no points unless the project opts in. Projects that
-- already gave points to bugs or chores keep working as before: they start
-- with the option on, so nothing already estimated loses its estimate.
ALTER TABLE projects ADD COLUMN estimate_bugs_and_chores BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE projects
SET estimate_bugs_and_chores = TRUE
WHERE id IN (SELECT project_id FROM stories WHERE type <> 'feature' AND estimate IS NOT NULL);

-- +goose Down
ALTER TABLE projects DROP COLUMN estimate_bugs_and_chores;
