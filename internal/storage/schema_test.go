package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func openSchemaTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "metadata.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

func TestSchemaCreatesTwoResourceTables(t *testing.T) {
	t.Parallel()
	db := openSchemaTestDB(t)
	rows, err := db.QueryContext(t.Context(), "SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tables, []string{"applications", "maintainers"}) {
		t.Fatalf("tables = %v, want applications and maintainers", tables)
	}

	for table, want := range map[string][]string{
		"maintainers":  {"id", "name", "email"},
		"applications": {"id", "title", "version", "company", "website", "source", "license", "description", "maintainer_id"},
	} {
		t.Run(table, func(t *testing.T) {
			rows, err := db.QueryContext(t.Context(), "SELECT name FROM pragma_table_info(?) ORDER BY cid", table)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var columns []string
			for rows.Next() {
				var name string
				if err := rows.Scan(&name); err != nil {
					t.Fatal(err)
				}
				columns = append(columns, name)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(columns, want) {
				t.Errorf("columns = %v, want %v", columns, want)
			}
		})
	}

	var indexedColumn string
	if err := db.QueryRowContext(t.Context(), "SELECT name FROM pragma_index_info('applications_maintainer_id_idx')").Scan(&indexedColumn); err != nil {
		t.Fatal(err)
	}
	if indexedColumn != "maintainer_id" {
		t.Fatalf("indexed column = %q, want maintainer_id", indexedColumn)
	}
}

func TestSchemaApplicationDefaults(t *testing.T) {
	t.Parallel()
	db := openSchemaTestDB(t)
	if _, err := db.ExecContext(t.Context(), "INSERT INTO maintainers (name, email) VALUES (?, ?)", "Alex", "alex@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "INSERT INTO applications (title, version, maintainer_id) VALUES (?, ?, ?)", "Explorer", "1.0", 1); err != nil {
		t.Fatal(err)
	}
	var id, maintainerID int64
	var company, website, source, license, description string
	if err := db.QueryRowContext(t.Context(), `
		SELECT id, maintainer_id, company, website, source, license, description
		FROM applications
	`).Scan(&id, &maintainerID, &company, &website, &source, &license, &description); err != nil {
		t.Fatal(err)
	}
	if id <= 0 || maintainerID != 1 {
		t.Errorf("application ID = %d, maintainer ID = %d", id, maintainerID)
	}
	if company != "" || website != "" || source != "" || license != "" || description != "" {
		t.Error("omitted optional fields must default to empty strings")
	}
}

func TestSchemaRejectsInvalidRecords(t *testing.T) {
	t.Parallel()
	db := openSchemaTestDB(t)
	if _, err := db.ExecContext(t.Context(), "INSERT INTO maintainers (name, email) VALUES (?, ?)", "Alex", "alex@example.com"); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		sql  string
		code int
	}{
		{name: "missing name", sql: "INSERT INTO maintainers (email) VALUES ('alex@example.com')", code: sqlite3.SQLITE_CONSTRAINT_NOTNULL},
		{name: "missing email", sql: "INSERT INTO maintainers (name) VALUES ('Alex')", code: sqlite3.SQLITE_CONSTRAINT_NOTNULL},
		{name: "nonpositive maintainer ID", sql: "INSERT INTO maintainers (id, name, email) VALUES (0, 'Alex', 'alex@example.com')", code: sqlite3.SQLITE_CONSTRAINT_CHECK},
		{name: "missing title", sql: "INSERT INTO applications (version, maintainer_id) VALUES ('1.0', 1)", code: sqlite3.SQLITE_CONSTRAINT_NOTNULL},
		{name: "missing version", sql: "INSERT INTO applications (title, maintainer_id) VALUES ('Explorer', 1)", code: sqlite3.SQLITE_CONSTRAINT_NOTNULL},
		{name: "missing maintainer", sql: "INSERT INTO applications (title, version) VALUES ('Explorer', '1.0')", code: sqlite3.SQLITE_CONSTRAINT_NOTNULL},
		{name: "unknown maintainer", sql: "INSERT INTO applications (title, version, maintainer_id) VALUES ('Explorer', '1.0', 99)", code: sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY},
		{name: "nonpositive application ID", sql: "INSERT INTO applications (id, title, version, maintainer_id) VALUES (0, 'Explorer', '1.0', 1)", code: sqlite3.SQLITE_CONSTRAINT_CHECK},
		{name: "null optional field", sql: "INSERT INTO applications (title, version, maintainer_id, company) VALUES ('Explorer', '1.0', 1, NULL)", code: sqlite3.SQLITE_CONSTRAINT_NOTNULL},
		{name: "non-text title", sql: "INSERT INTO applications (title, version, maintainer_id) VALUES (X'0102', '1.0', 1)", code: sqlite3.SQLITE_CONSTRAINT_DATATYPE},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := db.ExecContext(t.Context(), tt.sql)
			var sqliteErr *sqlite.Error
			if !errors.As(err, &sqliteErr) || sqliteErr.Code() != tt.code {
				t.Fatalf("error = %v, want SQLite code %d", err, tt.code)
			}
		})
	}
}

func TestSchemaAllowsDuplicateMetadataAndMultipleApplications(t *testing.T) {
	t.Parallel()
	db := openSchemaTestDB(t)
	for range 2 {
		if _, err := db.ExecContext(t.Context(), "INSERT INTO maintainers (name, email) VALUES (?, ?)", "Alex", "alex@example.com"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(t.Context(), "INSERT INTO applications (title, version, maintainer_id) VALUES (?, ?, ?)", "Explorer", "1.0", 1); err != nil {
			t.Fatal(err)
		}
	}
	var maintainerCount, applicationCount int
	if err := db.QueryRowContext(t.Context(), `
		SELECT (SELECT COUNT(*) FROM maintainers),
		       (SELECT COUNT(*) FROM applications WHERE maintainer_id = 1)
	`).Scan(&maintainerCount, &applicationCount); err != nil {
		t.Fatal(err)
	}
	if maintainerCount != 2 || applicationCount != 2 {
		t.Fatalf("maintainers = %d, applications for creator 1 = %d, want 2 each", maintainerCount, applicationCount)
	}
}

func TestSchemaRestrictsMaintainerDeletion(t *testing.T) {
	t.Parallel()
	db := openSchemaTestDB(t)
	if _, err := db.ExecContext(t.Context(), "INSERT INTO maintainers (name, email) VALUES (?, ?)", "Alex", "alex@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "INSERT INTO applications (title, version, maintainer_id) VALUES (?, ?, ?)", "Explorer", "1.0", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "DELETE FROM maintainers WHERE id = ?", 1); err == nil {
		t.Fatal("deleting a maintainer with applications must fail")
	}
	var count int
	if err := db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM applications JOIN maintainers ON applications.maintainer_id = maintainers.id").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("failed deletion must preserve both records")
	}
	if _, err := db.ExecContext(t.Context(), "DELETE FROM applications WHERE id = ?", 1); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM maintainers").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("deleting an application must preserve its maintainer")
	}
	if _, err := db.ExecContext(t.Context(), "DELETE FROM maintainers WHERE id = ?", 1); err != nil {
		t.Fatal(err)
	}
}

func TestSchemaInitializationRollsBackOnFailure(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "metadata.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.ExecContext(t.Context(), "CREATE TABLE applications (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}

	db, err := Open(t.Context(), path)
	if db != nil {
		db.Close()
		t.Fatal("incompatible schema must not return an open database")
	}
	if err == nil {
		t.Fatal("expected initialization to fail when the index column is missing")
	}
	var count int
	if err := raw.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM sqlite_schema WHERE name = 'maintainers'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("failed initialization left a partially created schema")
	}
}

func TestSchemaInitializationHonorsCancellation(t *testing.T) {
	t.Parallel()
	db := openSchemaTestDB(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := initializeSchema(ctx, db); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}

func TestSchemaInitializationReportsCommitFailure(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "metadata.db")
	reader, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err := reader.ExecContext(t.Context(), "CREATE TABLE existing (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	tx, err := reader.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM existing").Scan(&count); err != nil {
		t.Fatal(err)
	}

	// A shared read lock allows schema writes but blocks their rollback-journal commit.
	db, err := Open(t.Context(), path)
	if db != nil {
		db.Close()
		t.Fatal("failed schema commit must not return an open database")
	}
	var sqliteErr *sqlite.Error
	if err == nil || !strings.Contains(err.Error(), "commit schema initialization") ||
		!errors.As(err, &sqliteErr) || sqliteErr.Code()&0xff != sqlite3.SQLITE_BUSY {
		t.Fatalf("error = %v, want a schema commit failure caused by SQLite busy", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := reader.QueryRowContext(t.Context(), `
			SELECT COUNT(*) FROM sqlite_schema WHERE name IN ('maintainers', 'applications')
		`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed commit left %d partially initialized resource tables", count)
	}
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("startup did not recover after releasing the reader: %v", err)
	}
	defer reopened.Close()
	if err := reopened.QueryRowContext(t.Context(), `
			SELECT COUNT(*) FROM sqlite_schema WHERE name IN ('maintainers', 'applications')
		`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("recovered resource tables = %d, error = %v, want 2", count, err)
	}
}
