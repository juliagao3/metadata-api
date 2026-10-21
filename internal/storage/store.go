package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var (
	ErrNotFound        = errors.New("resource not found")
	ErrUnavailable     = errors.New("database unavailable")
	ErrMaintainerInUse = errors.New("maintainer has applications")
	ErrForeignKey      = errors.New("foreign key constraint failed")
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) PingContext(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func databaseError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %w", ErrNotFound, err)
	}
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		code := sqliteErr.Code() & 0xff
		if code == sqlite3.SQLITE_BUSY || code == sqlite3.SQLITE_LOCKED {
			return fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		// SQLite implements ON DELETE RESTRICT using a foreign-key constraint trigger.
		if sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY ||
			(sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_TRIGGER &&
				strings.Contains(sqliteErr.Error(), "FOREIGN KEY constraint failed")) {
			return fmt.Errorf("%w: %w", ErrForeignKey, err)
		}
	}
	return err
}
