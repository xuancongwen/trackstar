-- +goose Up
-- Projects gain owners: whoever creates a project owns it, and owners manage
-- members and settings. Roles are now owner | member; the read-only viewer
-- role is gone. Existing writers could already manage members, so in projects
-- without an owner they become owners; viewers become members. Projects with
-- no members stay open to everyone until an administrator adds an owner.
UPDATE project_members
SET role = 'owner'
WHERE role = 'member'
  AND project_id NOT IN (SELECT project_id FROM project_members WHERE role = 'owner');

UPDATE project_members SET role = 'member' WHERE role = 'viewer';

-- +goose Down
UPDATE project_members SET role = 'member' WHERE role = 'owner';
