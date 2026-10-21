package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenConnectionSettings(t *testing.T) {
	t.Parallel()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "metadata.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("maximum connections = %d, want 1", got)
	}

	for _, connection := range []string{"initial", "replacement"} {
		t.Run(connection, func(t *testing.T) {
			for pragma, want := range map[string]int{
				"foreign_keys": 1,
				"synchronous":  2,
				"busy_timeout": 1000,
			} {
				var got int
				if err := db.QueryRowContext(t.Context(), "PRAGMA "+pragma).Scan(&got); err != nil {
					t.Fatal(err)
				}
				if got != want {
					t.Errorf("%s = %d, want %d", pragma, got, want)
				}
			}
		})
		db.SetMaxIdleConns(0)
	}
}

func TestOpenPersistsDataAndEscapesFilename(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "metadata # & data.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.ExecContext(t.Context(), "INSERT INTO maintainers (name, email) VALUES (?, ?)", "Alex", "alex@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "INSERT INTO applications (title, version, maintainer_id) VALUES (?, ?, ?)", "Explorer", "1.0.0", 1); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database was not created at the requested path: %v", err)
	}

	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	var title, name string
	if err := reopened.QueryRowContext(t.Context(), `
		SELECT applications.title, maintainers.name
		FROM applications JOIN maintainers ON applications.maintainer_id = maintainers.id
	`).Scan(&title, &name); err != nil {
		t.Fatal(err)
	}
	if title != "Explorer" || name != "Alex" {
		t.Fatalf("reopened records = (%q, %q), want (Explorer, Alex)", title, name)
	}
}

func TestOpenRejectsInvalidPaths(t *testing.T) {
	t.Parallel()
	for name, path := range map[string]string{
		"empty":          "",
		"blank":          " \t",
		"directory":      t.TempDir(),
		"missing parent": filepath.Join(t.TempDir(), "missing", "metadata.db"),
	} {
		t.Run(name, func(t *testing.T) {
			db, err := Open(t.Context(), path)
			if db != nil {
				db.Close()
				t.Error("expected no database on failure")
			}
			if err == nil {
				t.Fatal("expected an error for an invalid database path")
			}
		})
	}
}

func TestOpenHonorsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	db, err := Open(ctx, filepath.Join(t.TempDir(), "metadata.db"))
	if db != nil {
		db.Close()
		t.Error("expected no database on cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}
