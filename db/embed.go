// Package db holds the SQL migrations and sqlc query sources.
package db

import "embed"

// SQLiteMigrations contains the goose migrations for the SQLite driver.
// A future PostgreSQL driver gets its own sibling directory.
//
//go:embed migrations/sqlite/*.sql
var SQLiteMigrations embed.FS

// SQLiteMigrationsDir is the directory inside SQLiteMigrations.
const SQLiteMigrationsDir = "migrations/sqlite"
