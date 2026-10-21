package storage

import (
	"context"
	"errors"
	"reflect"
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
