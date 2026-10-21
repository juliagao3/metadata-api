package storage

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDeleteApplicationPersistsAfterReopen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "metadata.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStore(db)
	owner, err := store.CreateMaintainer(t.Context(), "Alex", "alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	otherOwner, err := store.CreateMaintainer(t.Context(), "Sam", "sam@example.com")
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.CreateApplication(t.Context(), Application{Title: "First", Version: "1", MaintainerID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateApplication(t.Context(), Application{Title: "Second", Version: "1", MaintainerID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateApplication(t.Context(), Application{Title: "Other", Version: "1", MaintainerID: otherOwner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteApplication(t.Context(), first.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteApplication(t.Context(), first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("repeated deletion error = %v, want not found", err)
	}
	if err := store.DeleteApplication(t.Context(), other.ID+1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing deletion error = %v, want not found", err)
	}
	all, err := store.ListApplications(t.Context())
	if err != nil || !reflect.DeepEqual(all, []Application{second, other}) {
		t.Fatalf("unrelated applications changed: %+v, error = %v", all, err)
	}
	if err := store.DeleteMaintainer(t.Context(), owner.ID); !errors.Is(err, ErrMaintainerInUse) {
		t.Fatalf("creator must remain protected while the second application exists: %v", err)
	}
	if err := store.DeleteApplication(t.Context(), second.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	store = NewStore(reopened)
	for _, id := range []int64{first.ID, second.ID} {
		if _, err := store.GetApplication(t.Context(), id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("deleted application %d returned after restart: %v", id, err)
		}
	}
	for _, creator := range []Maintainer{owner, otherOwner} {
		got, err := store.GetMaintainer(t.Context(), creator.ID)
		if err != nil || got != creator {
			t.Fatalf("deleting applications must preserve creators: %+v, error = %v", got, err)
		}
	}
	if err := store.DeleteMaintainer(t.Context(), owner.ID); err != nil {
		t.Fatalf("creator must be deletable after all their applications are removed: %v", err)
	}
	all, err = store.ListApplications(t.Context())
	if err != nil || !reflect.DeepEqual(all, []Application{other}) {
		t.Fatalf("persisted applications = %+v, error = %v, want only the unrelated application", all, err)
	}
}

func TestDeleteApplicationHonorsCancellation(t *testing.T) {
	t.Parallel()
	store := NewStore(openSchemaTestDB(t))
	owner, err := store.CreateMaintainer(t.Context(), "Alex", "alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	original, err := store.CreateApplication(t.Context(), Application{Title: "App", Version: "1", MaintainerID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := store.DeleteApplication(ctx, original.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation", err)
	}
	saved, err := store.GetApplication(t.Context(), original.ID)
	if err != nil || saved != original {
		t.Fatalf("canceled deletion changed the application: %+v, error = %v", saved, err)
	}
}

func TestDeleteApplicationReportsClosedDatabase(t *testing.T) {
	t.Parallel()
	db := openSchemaTestDB(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	err := NewStore(db).DeleteApplication(t.Context(), 1)
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want unexpected storage failure", err)
	}
}

func TestDeleteApplicationReportsBusyDatabase(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "metadata.db")
	writerDB, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer writerDB.Close()
	writer := NewStore(writerDB)
	owner, err := writer.CreateMaintainer(t.Context(), "Alex", "alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	original, err := writer.CreateApplication(t.Context(), Application{Title: "App", Version: "1", MaintainerID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := writerDB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(t.Context(), "UPDATE applications SET title = ? WHERE id = ?", "Holding lock", original.ID); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	if err := store.DeleteApplication(t.Context(), original.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want unavailable", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	saved, err := store.GetApplication(t.Context(), original.ID)
	if err != nil || saved != original {
		t.Fatalf("failed deletion changed the application: %+v, error = %v", saved, err)
	}
	if err := store.DeleteApplication(t.Context(), original.ID); err != nil {
		t.Fatalf("delete must work after releasing the lock: %v", err)
	}
}

func TestUpdateApplicationPersistsAfterReopen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "metadata.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStore(db)
	owner, err := store.CreateMaintainer(t.Context(), "Alex", "alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	otherOwner, err := store.CreateMaintainer(t.Context(), "Sam", "sam@example.com")
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.CreateApplication(t.Context(), Application{Title: "First", Version: "1", MaintainerID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateApplication(t.Context(), Application{Title: "Other", Version: "1", MaintainerID: otherOwner.ID})
	if err != nil {
		t.Fatal(err)
	}
	input := Application{
		ID: other.ID, Title: " App O'Brien'); DROP TABLE applications; --", Version: " latest ",
		Company: " Example ", Website: "not a URL", Source: "local", License: "custom",
		Description: "First line\nSecond line", MaintainerID: otherOwner.ID,
	}
	want := input
	want.ID, want.MaintainerID = first.ID, owner.ID
	updated, err := store.UpdateApplication(t.Context(), first.ID, input)
	if err != nil || updated != want {
		t.Fatalf("updated = %+v, error = %v, want %+v with unchanged ID and creator", updated, err, want)
	}
	repeated, err := store.UpdateApplication(t.Context(), first.ID, input)
	if err != nil || repeated != want {
		t.Fatalf("identical PUT must succeed: %+v, error = %v", repeated, err)
	}
	saved, err := store.GetApplication(t.Context(), first.ID)
	if err != nil || saved != want {
		t.Fatalf("saved = %+v, error = %v, want %+v", saved, err, want)
	}
	cleared := Application{ID: first.ID, Title: other.Title, Version: other.Version, MaintainerID: owner.ID}
	updated, err = store.UpdateApplication(t.Context(), first.ID, Application{Title: other.Title, Version: other.Version})
	if err != nil || updated != cleared {
		t.Fatalf("omitted optional fields must clear, and duplicate metadata is allowed: %+v, error = %v", updated, err)
	}
	missing, err := store.UpdateApplication(t.Context(), other.ID+1, input)
	if !errors.Is(err, ErrNotFound) || missing != (Application{}) {
		t.Fatalf("application = %+v, error = %v, want not found without upsert", missing, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	store = NewStore(reopened)
	all, err := store.ListApplications(t.Context())
	if err != nil || !reflect.DeepEqual(all, []Application{cleared, other}) {
		t.Fatalf("persisted records = %+v, error = %v; unrelated record must remain unchanged", all, err)
	}
	for _, creator := range []Maintainer{owner, otherOwner} {
		got, err := store.GetMaintainer(t.Context(), creator.ID)
		if err != nil || got != creator {
			t.Fatalf("creator changed: %+v, error = %v", got, err)
		}
	}
	if err := store.DeleteMaintainer(t.Context(), owner.ID); !errors.Is(err, ErrMaintainerInUse) {
		t.Fatalf("original creator must remain protected: %v", err)
	}
}

func TestUpdateApplicationHonorsCancellation(t *testing.T) {
	t.Parallel()
	store := NewStore(openSchemaTestDB(t))
	owner, err := store.CreateMaintainer(t.Context(), "Alex", "alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	original, err := store.CreateApplication(t.Context(), Application{Title: "App", Version: "1", MaintainerID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := store.UpdateApplication(ctx, original.ID, Application{Title: "Changed", Version: "2"})
	if !errors.Is(err, context.Canceled) || got != (Application{}) {
		t.Fatalf("application = %+v, error = %v, want cancellation", got, err)
	}
	saved, err := store.GetApplication(t.Context(), original.ID)
	if err != nil || saved != original {
		t.Fatalf("canceled update changed the application: %+v, error = %v", saved, err)
	}
}

func TestUpdateApplicationReportsClosedDatabase(t *testing.T) {
	t.Parallel()
	db := openSchemaTestDB(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := NewStore(db).UpdateApplication(t.Context(), 1, Application{Title: "App", Version: "2"})
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnavailable) || got != (Application{}) {
		t.Fatalf("application = %+v, error = %v, want unexpected storage failure", got, err)
	}
}

func TestUpdateApplicationReportsBusyDatabase(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "metadata.db")
	writerDB, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer writerDB.Close()
	writer := NewStore(writerDB)
	owner, err := writer.CreateMaintainer(t.Context(), "Alex", "alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	original, err := writer.CreateApplication(t.Context(), Application{Title: "App", Version: "1", MaintainerID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := writerDB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(t.Context(), "UPDATE applications SET title = ? WHERE id = ?", "Holding lock", original.ID); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	input := Application{Title: "Updated", Version: "2"}
	got, err := store.UpdateApplication(t.Context(), original.ID, input)
	if !errors.Is(err, ErrUnavailable) || got != (Application{}) {
		t.Fatalf("application = %+v, error = %v, want unavailable", got, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	saved, err := store.GetApplication(t.Context(), original.ID)
	if err != nil || saved != original {
		t.Fatalf("failed update changed the application: %+v, error = %v", saved, err)
	}
	if _, err := store.UpdateApplication(t.Context(), original.ID, input); err != nil {
		t.Fatalf("update must work after releasing the lock: %v", err)
	}
}

func TestGetApplication(t *testing.T) {
	t.Parallel()
	store := NewStore(openSchemaTestDB(t))
	owner, err := store.CreateMaintainer(t.Context(), "Alex", "alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	inputs := []Application{
		{Title: " App ", Version: "latest", Company: "Example", Website: "https://example.com",
			Source: "local", License: "custom", Description: "Description\nSecond line", MaintainerID: owner.ID},
		{Title: "Minimal", Version: "1", MaintainerID: owner.ID},
	}
	var lastID int64
	for _, input := range inputs {
		want, err := store.CreateApplication(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		got, err := store.GetApplication(t.Context(), want.ID)
		if err != nil || got != want {
			t.Fatalf("application = %+v, error = %v, want %+v", got, err, want)
		}
		lastID = want.ID
	}
	got, err := store.GetApplication(t.Context(), lastID+1)
	if !errors.Is(err, ErrNotFound) || got != (Application{}) {
		t.Fatalf("application = %+v, error = %v, want not found", got, err)
	}
	savedOwner, err := store.GetMaintainer(t.Context(), owner.ID)
	if err != nil || savedOwner != owner {
		t.Fatalf("reads changed the maintainer: %+v, error = %v", savedOwner, err)
	}
}

func TestListApplications(t *testing.T) {
	t.Parallel()
	db := openSchemaTestDB(t)
	store := NewStore(db)
	empty, err := store.ListApplications(t.Context())
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty collection = %v, error = %v, want non-nil empty slice", empty, err)
	}
	firstOwner, err := store.CreateMaintainer(t.Context(), "Alex", "alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	secondOwner, err := store.CreateMaintainer(t.Context(), "Sam", "sam@example.com")
	if err != nil {
		t.Fatal(err)
	}
	want := []Application{
		{ID: 3, Title: "First", Version: "1", MaintainerID: secondOwner.ID},
		{ID: 20, Title: "Second", Version: "latest", Company: "Example", Website: "https://example.com",
			Source: "local", License: "custom", Description: "Description", MaintainerID: firstOwner.ID},
	}
	for _, input := range []Application{want[1], want[0]} {
		if _, err := db.ExecContext(t.Context(), `
			INSERT INTO applications (id, title, version, company, website, source, license, description, maintainer_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, input.ID, input.Title, input.Version, input.Company, input.Website,
			input.Source, input.License, input.Description, input.MaintainerID); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.ListApplications(t.Context())
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("applications = %+v, error = %v, want %+v in ascending ID order", got, err, want)
	}
}

func TestListApplicationsReportsRowFailures(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name       string
		projection string
		wantError  string
	}{
		{"scan", "CASE WHEN id = 2 THEN 'invalid' ELSE id END AS id, title", "read application:"},
		{"iteration", "id, CASE WHEN id = 2 THEN abs(-9223372036854775808) ELSE title END AS title", "list applications:"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db := openSchemaTestDB(t)
			store := NewStore(db)
			owner, err := store.CreateMaintainer(t.Context(), "Alex", "alex@example.com")
			if err != nil {
				t.Fatal(err)
			}
			for _, title := range []string{"First", "Second"} {
				if _, err := store.CreateApplication(t.Context(), Application{
					Title: title, Version: "1", MaintainerID: owner.ID,
				}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.ExecContext(t.Context(), `
				ALTER TABLE applications RENAME TO original_applications;
				CREATE VIEW applications AS SELECT `+tt.projection+`,
					version, company, website, source, license, description, maintainer_id
				FROM original_applications;
			`); err != nil {
				t.Fatal(err)
			}
			got, err := store.ListApplications(t.Context())
			if err == nil || !strings.HasPrefix(err.Error(), tt.wantError) || got != nil {
				t.Fatalf("list result = %+v, error = %v, want %q and no partial collection", got, err, tt.wantError)
			}
			if _, err := db.ExecContext(t.Context(), `
				DROP VIEW applications;
				ALTER TABLE original_applications RENAME TO applications;
			`); err != nil {
				t.Fatal(err)
			}
			recovered, err := store.ListApplications(t.Context())
			if err != nil || len(recovered) != 2 || recovered[0].Title != "First" || recovered[1].Title != "Second" {
				t.Fatalf("records or connection did not recover: %+v, error = %v", recovered, err)
			}
		})
	}
}

func TestApplicationReadsHonorCancellation(t *testing.T) {
	t.Parallel()
	store := NewStore(openSchemaTestDB(t))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got, err := store.GetApplication(ctx, 1); !errors.Is(err, context.Canceled) || got != (Application{}) {
		t.Errorf("get result = %+v, error = %v, want cancellation", got, err)
	}
	if got, err := store.ListApplications(ctx); !errors.Is(err, context.Canceled) || got != nil {
		t.Errorf("list result = %+v, error = %v, want cancellation", got, err)
	}
}

func TestApplicationReadsReportClosedDatabase(t *testing.T) {
	t.Parallel()
	db := openSchemaTestDB(t)
	store := NewStore(db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := store.GetApplication(t.Context(), 1); err == nil || errors.Is(err, ErrNotFound) || got != (Application{}) {
		t.Errorf("get result = %+v, error = %v, want unexpected storage failure", got, err)
	}
	if got, err := store.ListApplications(t.Context()); err == nil || got != nil {
		t.Errorf("list result = %+v, error = %v, want failure rather than empty success", got, err)
	}
}

func TestCreateApplicationPersistsAfterReopen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "metadata.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStore(db)
	owner, err := store.CreateMaintainer(t.Context(), "Alex", "alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	input := Application{
		ID: 999, Title: " App O'Brien'); DROP TABLE applications; --", Version: " latest ",
		Company: " Example ", Website: "not a URL", Source: "local source",
		License: "custom", Description: "First line\nSecond line", MaintainerID: owner.ID,
	}
	first, err := store.CreateApplication(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID <= 0 || first.ID == input.ID {
		t.Fatalf("ID must be server generated: %+v", first)
	}
	input.ID = first.ID
	if first != input {
		t.Fatalf("application = %+v, want %+v", first, input)
	}
	second, err := store.CreateApplication(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID <= 0 || second.ID == first.ID {
		t.Fatalf("duplicate metadata must create a distinct application: %+v", second)
	}
	minimal, err := store.CreateApplication(t.Context(), Application{Title: "Minimal", Version: "1", MaintainerID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if minimal.ID <= 0 || minimal.Company != "" || minimal.Website != "" || minimal.Source != "" ||
		minimal.License != "" || minimal.Description != "" {
		t.Fatalf("optional fields must default to empty strings: %+v", minimal)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var saved Application
	if err := reopened.QueryRowContext(t.Context(), `
		SELECT id, title, version, company, website, source, license, description, maintainer_id
		FROM applications WHERE id = ?
	`, first.ID).Scan(&saved.ID, &saved.Title, &saved.Version, &saved.Company, &saved.Website,
		&saved.Source, &saved.License, &saved.Description, &saved.MaintainerID); err != nil {
		t.Fatal(err)
	}
	if saved != first {
		t.Fatalf("persisted = %+v, want %+v", saved, first)
	}
	var count int
	if err := reopened.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM applications WHERE maintainer_id = ?", owner.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("application count = %d, want 3", count)
	}
	savedOwner, err := NewStore(reopened).GetMaintainer(t.Context(), owner.ID)
	if err != nil || savedOwner != owner {
		t.Fatalf("creator changed: %+v, error = %v", savedOwner, err)
	}
	if err := NewStore(reopened).DeleteMaintainer(t.Context(), owner.ID); !errors.Is(err, ErrMaintainerInUse) {
		t.Fatalf("creator must be protected from deletion: %v", err)
	}
}

func TestCreateApplicationRejectsMissingMaintainer(t *testing.T) {
	t.Parallel()
	db := openSchemaTestDB(t)
	application, err := NewStore(db).CreateApplication(t.Context(), Application{Title: "App", Version: "1", MaintainerID: 7})
	if !errors.Is(err, ErrForeignKey) || application != (Application{}) {
		t.Fatalf("application = %+v, error = %v, want foreign key error", application, err)
	}
	var count int
	if err := db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM applications").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("failed creation must not leave an orphan")
	}
}

func TestCreateApplicationHonorsCancellation(t *testing.T) {
	t.Parallel()
	db := openSchemaTestDB(t)
	store := NewStore(db)
	owner, err := store.CreateMaintainer(t.Context(), "Alex", "alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	application, err := store.CreateApplication(ctx, Application{Title: "App", Version: "1", MaintainerID: owner.ID})
	if !errors.Is(err, context.Canceled) || application != (Application{}) {
		t.Fatalf("application = %+v, error = %v, want cancellation", application, err)
	}
	var count int
	if err := db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM applications").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("canceled creation must not insert a record")
	}
}

func TestCreateApplicationReportsClosedDatabase(t *testing.T) {
	t.Parallel()
	db := openSchemaTestDB(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	application, err := NewStore(db).CreateApplication(t.Context(), Application{Title: "App", Version: "1", MaintainerID: 1})
	if err == nil || errors.Is(err, ErrForeignKey) || errors.Is(err, ErrUnavailable) || application != (Application{}) {
		t.Fatalf("application = %+v, error = %v, want unexpected storage failure", application, err)
	}
}

func TestCreateApplicationReportsBusyDatabase(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "metadata.db")
	writerDB, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer writerDB.Close()
	owner, err := NewStore(writerDB).CreateMaintainer(t.Context(), "Alex", "alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := writerDB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(t.Context(), "UPDATE maintainers SET name = ? WHERE id = ?", "Holding lock", owner.ID); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	input := Application{Title: "App", Version: "1", MaintainerID: owner.ID}
	application, err := store.CreateApplication(t.Context(), input)
	if !errors.Is(err, ErrUnavailable) || application != (Application{}) {
		t.Fatalf("application = %+v, error = %v, want unavailable", application, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM applications").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("blocked creation must not insert a record")
	}
	if _, err := store.CreateApplication(t.Context(), input); err != nil {
		t.Fatalf("creation must work after releasing the lock: %v", err)
	}
}
