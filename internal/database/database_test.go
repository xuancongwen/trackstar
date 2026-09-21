package database

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"tracker/internal/config"
	"tracker/internal/database/dbgen"
)

func TestMigrationsApplyAndAreIdempotent(t *testing.T) {
	ctx := context.Background()
	d := NewTestDB(t)

	version, err := d.SchemaVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if version < 1 {
		t.Fatalf("schema version = %d, want >= 1", version)
	}
	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	var mode string
	if err := d.sql.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
	var fk int
	if err := d.sql.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign_keys = %d (%v), want 1", fk, err)
	}
}

func TestInTxRollsBackOnError(t *testing.T) {
	ctx := context.Background()
	d := NewTestDB(t)
	boom := errors.New("boom")

	err := d.InTx(ctx, func(q dbgen.Querier) error {
		if _, err := q.CreateUser(ctx, dbgen.CreateUserParams{Email: "a@example.com", PasswordHash: "x", DisplayName: "A", Now: 1}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if n, _ := d.CountUsers(ctx); n != 0 {
		t.Fatalf("users = %d after rollback, want 0", n)
	}
}

func TestBackupProducesUsableCopy(t *testing.T) {
	ctx := context.Background()
	d := NewTestDB(t)
	if _, err := d.CreateUser(ctx, dbgen.CreateUserParams{Email: "a@example.com", PasswordHash: "x", DisplayName: "A", Now: 1}); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "backup.db")
	if err := d.Backup(ctx, dest); err != nil {
		t.Fatal(err)
	}
	copyDB, err := Open(ctx, config.DriverSQLite, dest)
	if err != nil {
		t.Fatal(err)
	}
	defer copyDB.Close()
	if n, _ := copyDB.CountUsers(ctx); n != 1 {
		t.Fatalf("users in backup = %d, want 1", n)
	}
}
