package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

type Maintainer struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (s *Store) CreateMaintainer(ctx context.Context, name, email string) (Maintainer, error) {
	var maintainer Maintainer
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO maintainers (name, email) VALUES (?, ?)
		RETURNING id, name, email
	`, name, email).Scan(&maintainer.ID, &maintainer.Name, &maintainer.Email)
	if err != nil {
		return Maintainer{}, fmt.Errorf("create maintainer: %w", databaseError(err))
	}
	return maintainer, nil
}

func (s *Store) GetMaintainer(ctx context.Context, id int64) (Maintainer, error) {
	var maintainer Maintainer
	err := s.db.QueryRowContext(ctx, "SELECT id, name, email FROM maintainers WHERE id = ?", id).
		Scan(&maintainer.ID, &maintainer.Name, &maintainer.Email)
	if err != nil {
		return Maintainer{}, fmt.Errorf("get maintainer: %w", databaseError(err))
	}
	return maintainer, nil
}

func (s *Store) UpdateMaintainer(ctx context.Context, id int64, name, email string) (Maintainer, error) {
	var maintainer Maintainer
	err := s.db.QueryRowContext(ctx, `
		UPDATE maintainers SET name = ?, email = ? WHERE id = ?
		RETURNING id, name, email
	`, name, email, id).Scan(&maintainer.ID, &maintainer.Name, &maintainer.Email)
	if err != nil {
		return Maintainer{}, fmt.Errorf("update maintainer: %w", databaseError(err))
	}
	return maintainer, nil
}

func (s *Store) DeleteMaintainer(ctx context.Context, id int64) error {
	var deletedID int64
	err := s.db.QueryRowContext(ctx, "DELETE FROM maintainers WHERE id = ? RETURNING id", id).Scan(&deletedID)
	if err != nil {
		var sqliteErr *sqlite.Error
		if errors.As(err, &sqliteErr) {
			// SQLite implements ON DELETE RESTRICT using a foreign-key constraint trigger.
			if sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY ||
				(sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_TRIGGER &&
					strings.Contains(sqliteErr.Error(), "FOREIGN KEY constraint failed")) {
				return fmt.Errorf("delete maintainer: %w: %w", ErrMaintainerInUse, err)
			}
		}
		return fmt.Errorf("delete maintainer: %w", databaseError(err))
	}
	return nil
}

func (s *Store) ListMaintainers(ctx context.Context) ([]Maintainer, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, email FROM maintainers ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("list maintainers: %w", databaseError(err))
	}
	defer rows.Close()

	maintainers := make([]Maintainer, 0)
	for rows.Next() {
		var maintainer Maintainer
		if err := rows.Scan(&maintainer.ID, &maintainer.Name, &maintainer.Email); err != nil {
			return nil, fmt.Errorf("read maintainer: %w", databaseError(err))
		}
		maintainers = append(maintainers, maintainer)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list maintainers: %w", databaseError(err))
	}
	return maintainers, nil
}
