package storage

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestGetMaintainer(t *testing.T) {
	t.Parallel()
	db := openSchemaTestDB(t)
	store := NewStore(db)
	first, err := store.CreateMaintainer(t.Context(), "Alex", "alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateMaintainer(t.Context(), "Sam", "sam@example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []Maintainer{first, second} {
		got, err := store.GetMaintainer(t.Context(), want.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("maintainer = %+v, want %+v", got, want)
		}
	}
	got, err := store.GetMaintainer(t.Context(), second.ID+1)
	if !errors.Is(err, ErrNotFound) || got != (Maintainer{}) {
		t.Fatalf("result = %+v, error = %v, want not found and no resource", got, err)
	}
}

func TestListMaintainers(t *testing.T) {
	t.Parallel()
	db := openSchemaTestDB(t)
	store := NewStore(db)
	empty, err := store.ListMaintainers(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty collection = %v, want a non-nil empty slice", empty)
	}

	want := []Maintainer{
		{ID: 3, Name: "Alex", Email: "alex@example.com"},
		{ID: 20, Name: "Sam", Email: "sam@example.com"},
	}
	for _, maintainer := range []Maintainer{want[1], want[0]} {
		if _, err := db.ExecContext(t.Context(), "INSERT INTO maintainers (id, name, email) VALUES (?, ?, ?)",
			maintainer.ID, maintainer.Name, maintainer.Email); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.ListMaintainers(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("maintainers = %+v, want %+v in ascending ID order", got, want)
	}
}

func TestListMaintainersReportsRowFailures(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name       string
		projection string
		wantError  string
	}{
		{"scan", "CASE WHEN id = 2 THEN 'invalid' ELSE id END AS id, name, email", "read maintainer:"},
		{"iteration", "id, CASE WHEN id = 2 THEN abs(-9223372036854775808) ELSE name END AS name, email", "list maintainers:"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db := openSchemaTestDB(t)
			store := NewStore(db)
			for _, name := range []string{"First", "Second"} {
				if _, err := store.CreateMaintainer(t.Context(), name, "alex@example.com"); err != nil {
					t.Fatal(err)
				}
			}
			// The view injects a bad second row using real SQLite, without a mock driver.
			if _, err := db.ExecContext(t.Context(), `
				ALTER TABLE maintainers RENAME TO original_maintainers;
				CREATE VIEW maintainers AS SELECT `+tt.projection+` FROM original_maintainers;
			`); err != nil {
				t.Fatal(err)
			}
			got, err := store.ListMaintainers(t.Context())
			if err == nil || !strings.HasPrefix(err.Error(), tt.wantError) || got != nil {
				t.Fatalf("list result = %+v, error = %v, want %q and no partial collection", got, err, tt.wantError)
			}
			if _, err := db.ExecContext(t.Context(), `
				DROP VIEW maintainers;
				ALTER TABLE original_maintainers RENAME TO maintainers;
			`); err != nil {
				t.Fatal(err)
			}
			recovered, err := store.ListMaintainers(t.Context())
			if err != nil || len(recovered) != 2 || recovered[0].Name != "First" || recovered[1].Name != "Second" {
				t.Fatalf("records or connection did not recover: %+v, error = %v", recovered, err)
			}
		})
	}
}

func TestMaintainerReadsHonorCancellation(t *testing.T) {
	t.Parallel()
	store := NewStore(openSchemaTestDB(t))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.GetMaintainer(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Errorf("get error = %v, want cancellation", err)
	}
	if _, err := store.ListMaintainers(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("list error = %v, want cancellation", err)
	}
}

func TestMaintainerReadsReportClosedDatabase(t *testing.T) {
	t.Parallel()
	db := openSchemaTestDB(t)
	store := NewStore(db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetMaintainer(t.Context(), 1); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("get error = %v, want a storage failure rather than not found", err)
	}
	if got, err := store.ListMaintainers(t.Context()); err == nil || got != nil {
		t.Errorf("list result = %v, error = %v, want failure rather than an empty success", got, err)
	}
}
