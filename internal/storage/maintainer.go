package storage

import (
	"context"
	"fmt"
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
