package database

import (
	"context"
	"path/filepath"
	"testing"

	"trackstar/internal/config"
)

// NewTestDB returns a migrated temporary SQLite database that is removed
// when the test finishes.
func NewTestDB(t testing.TB) *DB {
	t.Helper()
	ctx := context.Background()
	d, err := Open(ctx, config.DriverSQLite, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	return d
}
