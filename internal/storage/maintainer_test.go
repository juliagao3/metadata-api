package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestCreateMaintainerPersistsAfterReopen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "metadata.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStore(db)
	if err := store.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}

	wantName := "Alex O'Brien'); DROP TABLE maintainers; --"
	first, err := store.CreateMaintainer(t.Context(), wantName, "Alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID <= 0 || first.Name != wantName || first.Email != "Alex@example.com" {
		t.Fatalf("unexpected maintainer: %+v", first)
	}
	second, err := store.CreateMaintainer(t.Context(), wantName, first.Email)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID <= 0 || second.ID == first.ID {
		t.Fatalf("duplicate metadata must create a distinct resource: %+v", second)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var saved Maintainer
	if err := reopened.QueryRowContext(t.Context(), "SELECT id, name, email FROM maintainers WHERE id = ?", first.ID).
		Scan(&saved.ID, &saved.Name, &saved.Email); err != nil {
		t.Fatal(err)
	}
	if saved != first {
		t.Fatalf("saved = %+v, want %+v", saved, first)
	}
	var count int
	if err := reopened.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM maintainers").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("record count = %d, want 2", count)
	}
}

func TestCreateMaintainerHonorsCancellation(t *testing.T) {
	t.Parallel()
	db := openSchemaTestDB(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := NewStore(db).CreateMaintainer(ctx, "Alex", "alex@example.com")
	if !errors.Is(err, context.Canceled) || got != (Maintainer{}) {
		t.Fatalf("result = %+v, error = %v, want cancellation and no resource", got, err)
	}
	var count int
	if err := db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM maintainers").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("canceled creation must not insert a record")
	}
}

func TestCreateMaintainerReportsClosedDatabase(t *testing.T) {
	t.Parallel()
	db := openSchemaTestDB(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := NewStore(db).CreateMaintainer(t.Context(), "Alex", "alex@example.com")
	if err == nil || got != (Maintainer{}) {
		t.Fatalf("result = %+v, error = %v, want failure and no resource", got, err)
	}
	if errors.Is(err, ErrUnavailable) {
		t.Fatal("closed database is not a lock-contention error")
	}
}

func TestCreateMaintainerReportsBusyDatabase(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "metadata.db")
	writerDB, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer writerDB.Close()
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
	if _, err := tx.ExecContext(t.Context(), "INSERT INTO maintainers (name, email) VALUES (?, ?)", "Holding lock", "lock@example.com"); err != nil {
		t.Fatal(err)
	}

	store := NewStore(db)
	got, err := store.CreateMaintainer(t.Context(), "Alex", "alex@example.com")
	if !errors.Is(err, ErrUnavailable) || got != (Maintainer{}) {
		t.Fatalf("result = %+v, error = %v, want unavailable and no resource", got, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateMaintainer(t.Context(), "Alex", "alex@example.com"); err != nil {
		t.Fatalf("creation must work after the lock is released: %v", err)
	}
}
