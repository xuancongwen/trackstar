package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // pure-Go driver: no cgo, static binaries

	"tracker/db"
	"tracker/internal/database/dbgen"
)

func openSQLite(ctx context.Context, path string) (*DB, error) {
	// _txlock=immediate takes the write lock when a transaction begins, so
	// read-modify-write transactions (story moves) never hit SQLITE_BUSY on
	// lock upgrade; busy_timeout queues concurrent writers instead of failing.
	params := url.Values{}
	params.Set("_txlock", "immediate")
	for _, pragma := range []string{
		"journal_mode(WAL)",
		"synchronous(NORMAL)",
		"foreign_keys(ON)",
		"busy_timeout(5000)",
		"cache_size(-8000)", // 8 MB page cache keeps the memory footprint small
	} {
		params.Add("_pragma", pragma)
	}
	sqlDB, err := sql.Open("sqlite", "file:"+path+"?"+params.Encode())
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(4)
	sqlDB.SetMaxIdleConns(2)
	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("open sqlite database %s: %w", path, err)
	}
	return &DB{Queries: dbgen.New(sqlDB), sql: sqlDB, driver: "sqlite", path: path}, nil
}

// Migrate applies all pending migrations.
func (d *DB) Migrate(ctx context.Context) error {
	provider, err := goose.NewProvider(goose.DialectSQLite3, d.sql, mustSub(db.SQLiteMigrations, db.SQLiteMigrationsDir))
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}

// SchemaVersion returns the current migration version.
func (d *DB) SchemaVersion(ctx context.Context) (int64, error) {
	provider, err := goose.NewProvider(goose.DialectSQLite3, d.sql, mustSub(db.SQLiteMigrations, db.SQLiteMigrationsDir))
	if err != nil {
		return 0, err
	}
	return provider.GetDBVersion(ctx)
}

// Backup writes a consistent snapshot to dest using VACUUM INTO, which is
// safe while other connections (including the running server) keep writing.
func (d *DB) Backup(ctx context.Context, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("backup destination %s already exists", dest)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, err := d.sql.ExecContext(ctx, "VACUUM INTO '"+strings.ReplaceAll(dest, "'", "''")+"'")
	return err
}

// SizeBytes reports the on-disk size of the database (main file plus WAL).
func (d *DB) SizeBytes() int64 {
	var total int64
	for _, suffix := range []string{"", "-wal"} {
		if info, err := os.Stat(d.path + suffix); err == nil {
			total += info.Size()
		}
	}
	return total
}

// Health verifies that the database actually answers queries.
func (d *DB) Health(ctx context.Context) error {
	var one int
	return d.sql.QueryRowContext(ctx, "SELECT 1").Scan(&one)
}

// VerifySQLiteFile checks a database file that is not in use (a backup about
// to be restored): it must pass SQLite's integrity check and contain a
// tracker schema. It returns the schema version found.
func VerifySQLiteFile(ctx context.Context, path string) (int64, error) {
	if _, err := os.Stat(path); err != nil {
		return 0, err
	}
	sqlDB, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return 0, err
	}
	defer sqlDB.Close()

	var result string
	if err := sqlDB.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return 0, fmt.Errorf("integrity check: %w", err)
	}
	if result != "ok" {
		return 0, fmt.Errorf("integrity check failed: %s", result)
	}
	var version int64
	err = sqlDB.QueryRowContext(ctx, "SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied").Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("not a tracker database: %w", err)
	}
	return version, nil
}
