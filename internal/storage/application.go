package storage

import (
	"context"
	"fmt"
)

type Application struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	Version      string `json:"version"`
	Company      string `json:"company"`
	Website      string `json:"website"`
	Source       string `json:"source"`
	License      string `json:"license"`
	Description  string `json:"description"`
	MaintainerID int64  `json:"maintainer_id"`
}

func (s *Store) CreateApplication(ctx context.Context, input Application) (Application, error) {
	var application Application
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO applications (title, version, company, website, source, license, description, maintainer_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id, title, version, company, website, source, license, description, maintainer_id
	`, input.Title, input.Version, input.Company, input.Website, input.Source, input.License, input.Description, input.MaintainerID).
		Scan(&application.ID, &application.Title, &application.Version, &application.Company,
			&application.Website, &application.Source, &application.License, &application.Description, &application.MaintainerID)
	if err != nil {
		return Application{}, fmt.Errorf("create application: %w", databaseError(err))
	}
	return application, nil
}

func (s *Store) GetApplication(ctx context.Context, id int64) (Application, error) {
	var application Application
	err := s.db.QueryRowContext(ctx, `
		SELECT id, title, version, company, website, source, license, description, maintainer_id
		FROM applications WHERE id = ?
	`, id).Scan(&application.ID, &application.Title, &application.Version, &application.Company,
		&application.Website, &application.Source, &application.License, &application.Description, &application.MaintainerID)
	if err != nil {
		return Application{}, fmt.Errorf("get application: %w", databaseError(err))
	}
	return application, nil
}

func (s *Store) ListApplications(ctx context.Context) ([]Application, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, version, company, website, source, license, description, maintainer_id
		FROM applications ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("list applications: %w", databaseError(err))
	}
	defer rows.Close()

	applications := make([]Application, 0)
	for rows.Next() {
		var application Application
		if err := rows.Scan(&application.ID, &application.Title, &application.Version, &application.Company,
			&application.Website, &application.Source, &application.License, &application.Description, &application.MaintainerID); err != nil {
			return nil, fmt.Errorf("read application: %w", databaseError(err))
		}
		applications = append(applications, application)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list applications: %w", databaseError(err))
	}
	return applications, nil
}
