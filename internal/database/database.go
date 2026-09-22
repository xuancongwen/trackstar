// Package database owns the connection, migrations and transaction handling.
//
// Application code depends only on Store and dbgen.Querier. Everything that
// is specific to one database engine (DSN flags, pragmas, the migration
// dialect, backup) lives in a driver file in this package, so adding
// PostgreSQL means adding postgres.go plus generated queries — services,
// handlers and tests stay untouched.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"trackstar/internal/config"
	"trackstar/internal/database/dbgen"
)

// Store is the small persistence interface the services are written against.
type Store interface {
	dbgen.Querier
	// InTx runs fn inside one transaction; returning an error rolls it back.
	InTx(ctx context.Context, fn func(q dbgen.Querier) error) error
}

// DB is the Store implementation on top of database/sql.
type DB struct {
	*dbgen.Queries
	sql    *sql.DB
	driver string
	path   string
}

// Open connects according to the configuration. It does not run migrations.
func Open(ctx context.Context, driver, url string) (*DB, error) {
	switch driver {
	case config.DriverSQLite:
		return openSQLite(ctx, url)
	default:
		return nil, fmt.Errorf("unsupported database driver %q", driver)
	}
}

func (d *DB) InTx(ctx context.Context, fn func(q dbgen.Querier) error) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(d.Queries.WithTx(tx)); err != nil {
		return errors.Join(err, ignoreTxDone(tx.Rollback()))
	}
	return tx.Commit()
}

func ignoreTxDone(err error) error {
	if errors.Is(err, sql.ErrTxDone) {
		return nil
	}
	return err
}

func (d *DB) Ping(ctx context.Context) error { return d.sql.PingContext(ctx) }
func (d *DB) Close() error                   { return d.sql.Close() }
func (d *DB) Driver() string                 { return d.driver }

// IsNotFound reports whether err means "no such row".
func IsNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }
