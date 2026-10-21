package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestDeleteMaintainerPersistsAfterReopen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "metadata.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStore(db)
	first, err := store.CreateMaintainer(t.Context(), "Alex", "alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateMaintainer(t.Context(), "Sam", "sam@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteMaintainer(t.Context(), first.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteMaintainer(t.Context(), first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("repeated deletion error = %v, want not found", err)
	}
	if err := store.DeleteMaintainer(t.Context(), second.ID+1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing deletion error = %v, want not found", err)
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
	if _, err := store.GetMaintainer(t.Context(), first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted resource returned after restart: %v", err)
	}
	all, err := store.ListMaintainers(t.Context())
	if err != nil || len(all) != 1 || all[0] != second {
		t.Fatalf("unrelated record changed: %+v, error = %v", all, err)
	}
}

func TestDeleteMaintainerInUse(t *testing.T) {
	t.Parallel()
	db := openSchemaTestDB(t)
	store := NewStore(db)
	owner, err := store.CreateMaintainer(t.Context(), "Alex", "alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"First", "Second"} {
		if _, err := db.ExecContext(t.Context(), "INSERT INTO applications (title, version, maintainer_id) VALUES (?, ?, ?)", title, "1.0", owner.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.DeleteMaintainer(t.Context(), owner.ID); !errors.Is(err, ErrMaintainerInUse) {
		t.Fatalf("error = %v, want maintainer in use", err)
	}
	saved, err := store.GetMaintainer(t.Context(), owner.ID)
	if err != nil || saved != owner {
		t.Fatalf("owner changed after rejected deletion: %+v, error = %v", saved, err)
	}
	var count int
	if err := db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM applications WHERE maintainer_id = ?", owner.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("applications changed after rejected deletion: count = %d", count)
	}
	if _, err := db.ExecContext(t.Context(), "DELETE FROM applications WHERE maintainer_id = ?", owner.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteMaintainer(t.Context(), owner.ID); err != nil {
		t.Fatalf("delete after removing applications: %v", err)
	}
}

func TestDeleteMaintainerHonorsCancellation(t *testing.T) {
	t.Parallel()
	store := NewStore(openSchemaTestDB(t))
	original, err := store.CreateMaintainer(t.Context(), "Alex", "alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := store.DeleteMaintainer(ctx, original.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation", err)
	}
	saved, err := store.GetMaintainer(t.Context(), original.ID)
	if err != nil || saved != original {
		t.Fatalf("canceled deletion changed the record: %+v, error = %v", saved, err)
	}
}

func TestDeleteMaintainerReportsClosedDatabase(t *testing.T) {
	t.Parallel()
	db := openSchemaTestDB(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	err := NewStore(db).DeleteMaintainer(t.Context(), 1)
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrMaintainerInUse) || errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want an unexpected storage failure", err)
	}
}

func TestDeleteMaintainerReportsUnrelatedConstraint(t *testing.T) {
	t.Parallel()
	db := openSchemaTestDB(t)
	store := NewStore(db)
	original, err := store.CreateMaintainer(t.Context(), "Alex", "alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `
		CREATE TRIGGER block_deletion BEFORE DELETE ON maintainers
		BEGIN SELECT RAISE(ABORT, 'unrelated constraint'); END
	`); err != nil {
		t.Fatal(err)
	}
	err = store.DeleteMaintainer(t.Context(), original.ID)
	if err == nil || errors.Is(err, ErrMaintainerInUse) || errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want an unexpected storage failure", err)
	}
	saved, err := store.GetMaintainer(t.Context(), original.ID)
	if err != nil || saved != original {
		t.Fatalf("failed deletion changed the record: %+v, error = %v", saved, err)
	}
}

func TestDeleteMaintainerReportsBusyDatabase(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "metadata.db")
	writerDB, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer writerDB.Close()
	original, err := NewStore(writerDB).CreateMaintainer(t.Context(), "Alex", "alex@example.com")
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
	if _, err := tx.ExecContext(t.Context(), "UPDATE maintainers SET name = ? WHERE id = ?", "Holding lock", original.ID); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	if err := store.DeleteMaintainer(t.Context(), original.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want unavailable", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	saved, err := store.GetMaintainer(t.Context(), original.ID)
	if err != nil || saved != original {
		t.Fatalf("failed deletion changed the record: %+v, error = %v", saved, err)
	}
	if err := store.DeleteMaintainer(t.Context(), original.ID); err != nil {
		t.Fatalf("delete must work after releasing the lock: %v", err)
	}
}

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

func TestUpdateMaintainer(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "metadata.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStore(db)
	first, err := store.CreateMaintainer(t.Context(), "Alex", "alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateMaintainer(t.Context(), "Sam", "sam@example.com")
	if err != nil {
		t.Fatal(err)
	}
	want := Maintainer{ID: first.ID, Name: "Alex O'Brien'); DROP TABLE maintainers; --", Email: second.Email}
	got, err := store.UpdateMaintainer(t.Context(), first.ID, want.Name, want.Email)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("updated = %+v, want %+v", got, want)
	}
	saved, err := store.GetMaintainer(t.Context(), first.ID)
	if err != nil || saved != want {
		t.Fatalf("saved = %+v, error = %v, want %+v", saved, err, want)
	}
	unchanged, err := store.GetMaintainer(t.Context(), second.ID)
	if err != nil || unchanged != second {
		t.Fatalf("unrelated maintainer changed: %+v, error = %v", unchanged, err)
	}
	repeated, err := store.UpdateMaintainer(t.Context(), first.ID, want.Name, want.Email)
	if err != nil || repeated != want {
		t.Fatalf("identical PUT must succeed: %+v, error = %v", repeated, err)
	}
	missing, err := store.UpdateMaintainer(t.Context(), second.ID+1, "Missing", "missing@example.com")
	if !errors.Is(err, ErrNotFound) || missing != (Maintainer{}) {
		t.Fatalf("result = %+v, error = %v, want not found", missing, err)
	}
	all, err := store.ListMaintainers(t.Context())
	if err != nil || len(all) != 2 {
		t.Fatalf("missing-resource update must not insert: records = %v, error = %v", all, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, err := NewStore(reopened).GetMaintainer(t.Context(), first.ID)
	if err != nil || persisted != want {
		t.Fatalf("after reopen = %+v, error = %v, want %+v", persisted, err, want)
	}
}

func TestUpdateMaintainerHonorsCancellation(t *testing.T) {
	t.Parallel()
	store := NewStore(openSchemaTestDB(t))
	original, err := store.CreateMaintainer(t.Context(), "Alex", "alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := store.UpdateMaintainer(ctx, original.ID, "Changed", "changed@example.com")
	if !errors.Is(err, context.Canceled) || got != (Maintainer{}) {
		t.Fatalf("result = %+v, error = %v, want cancellation", got, err)
	}
	saved, err := store.GetMaintainer(t.Context(), original.ID)
	if err != nil || saved != original {
		t.Fatalf("canceled update changed the record: %+v, error = %v", saved, err)
	}
}

func TestUpdateMaintainerReportsClosedDatabase(t *testing.T) {
	t.Parallel()
	db := openSchemaTestDB(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := NewStore(db).UpdateMaintainer(t.Context(), 1, "Alex", "alex@example.com")
	if err == nil || errors.Is(err, ErrNotFound) || got != (Maintainer{}) {
		t.Fatalf("result = %+v, error = %v, want a storage failure", got, err)
	}
}

func TestUpdateMaintainerReportsBusyDatabase(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "metadata.db")
	writerDB, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer writerDB.Close()
	original, err := NewStore(writerDB).CreateMaintainer(t.Context(), "Alex", "alex@example.com")
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
	if _, err := tx.ExecContext(t.Context(), "UPDATE maintainers SET name = ? WHERE id = ?", "Holding lock", original.ID); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	got, err := store.UpdateMaintainer(t.Context(), original.ID, "Updated", "updated@example.com")
	if !errors.Is(err, ErrUnavailable) || got != (Maintainer{}) {
		t.Fatalf("result = %+v, error = %v, want unavailable", got, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	saved, err := store.GetMaintainer(t.Context(), original.ID)
	if err != nil || saved != original {
		t.Fatalf("failed update changed the record: %+v, error = %v", saved, err)
	}
	if _, err := store.UpdateMaintainer(t.Context(), original.ID, "Updated", "updated@example.com"); err != nil {
		t.Fatalf("update must work after the lock is released: %v", err)
	}
}
