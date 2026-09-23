package database

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"

	"trackstar/db"
	"trackstar/internal/config"
	"trackstar/internal/database/dbgen"
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
	if v, err := VerifySQLiteFile(ctx, dest); err != nil || v < 1 {
		t.Fatalf("VerifySQLiteFile = %d, %v", v, err)
	}
	if _, err := VerifySQLiteFile(ctx, filepath.Join(t.TempDir(), "missing.db")); err == nil {
		t.Fatal("expected an error for a missing file")
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

// Migration 00005 turns the writers of owner-less projects into owners and
// retires the viewer role; projects that already have an owner are untouched.
func TestProjectOwnerBackfill(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, config.DriverSQLite, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	provider, err := goose.NewProvider(goose.DialectSQLite3, d.sql, mustSub(db.SQLiteMigrations, db.SQLiteMigrationsDir))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 4); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`INSERT INTO users (id, email, password_hash, display_name, created_at, updated_at) VALUES (1,'a','x','a',0,0),(2,'b','x','b',0,0),(3,'c','x','c',0,0)`,
		`INSERT INTO projects (id, name, slug, created_at, updated_at) VALUES (1,'legacy','legacy',0,0),(2,'owned','owned',0,0),(3,'viewers','viewers',0,0),(4,'open','open',0,0)`,
		`INSERT INTO project_members (project_id, user_id, role) VALUES
			(1,1,'member'),(1,2,'member'),(1,3,'viewer'),
			(2,1,'owner'),(2,2,'member'),
			(3,3,'viewer')`,
	} {
		if _, err := d.sql.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	rows, err := d.sql.QueryContext(ctx, "SELECT project_id, user_id, role FROM project_members ORDER BY project_id, user_id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var p, u int64
		var role string
		if err := rows.Scan(&p, &u, &role); err != nil {
			t.Fatal(err)
		}
		got = append(got, fmt.Sprintf("%d/%d=%s", p, u, role))
	}
	want := []string{"1/1=owner", "1/2=owner", "1/3=member", "2/1=owner", "2/2=member", "3/3=member"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("members after backfill = %v, want %v", got, want)
	}
}
