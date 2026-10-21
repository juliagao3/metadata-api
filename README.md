# Metadata API

A RESTful HTTP API for managing Applications and the Maintainers who created them.

**Status:** Server foundation, resource schema, full CRUD for Maintainers and
Applications, and the E2E suite are implemented. The CI workflow is prepared;
its first GitHub-hosted run is pending.

## Quick start

Install [Go 1.27 or later](https://go.dev/doc/install) and ensure `go` is on your
PATH. No separate database server or C compiler is needed.

From the repository root, start the server:

```powershell
go run .\cmd\server
```

Go downloads the pinned dependencies on the first run. The default address is
`127.0.0.1:8080`; the default database is `metadata.db` in the current directory.
Check readiness from another terminal:

```powershell
curl.exe http://127.0.0.1:8080/readyz
```

Expected response: `200 OK` with `{"status":"ready"}`. Stop with Ctrl+C; the server
allows up to five seconds for active requests to finish before forcing shutdown.

Optional configuration:

```powershell
go run .\cmd\server -addr 127.0.0.1:8081 -db .\metadata.db
```

The database's parent directory must already exist. Its file and tables are
created automatically; startup failures are logged and exit nonzero.

Build a standalone Windows executable:

```powershell
go build -o .\bin\metadata-api.exe .\cmd\server
.\bin\metadata-api.exe
```

Commands target PowerShell on Windows; on Unix-like systems, use `curl`,
`./cmd/server` and omit the `.exe` extension when building.

## API reference

The [OpenAPI reference](docs/openapi.yaml) is the source of truth for endpoints,
schemas, validation, example requests and responses, update and deletion
semantics, and error codes. The API uses JSON and the `/api/v1` prefix.

Both `/api/v1/maintainers` and `/api/v1/applications` support POST/GET on
collections and GET/PUT/DELETE on `/{id}`. GET routes also support HEAD.
Application creators are immutable; PUT clears omitted optional metadata and
never upserts. Maintainers with applications cannot be deleted.

## Production features

- **Readiness and HTTP timeouts:** Detect unavailable storage and bound stalled
  connections; graceful shutdown lets active requests finish.
- **Strict inputs:** A 64 KiB body limit, JSON field/type checks, required fields,
  email validation, and string-length limits reject invalid writes.
- **Storage safety:** Parameterized SQL, transactional schema initialization, and
  foreign keys prevent SQL injection, partial initialization, and orphaned data.
- **Explicit errors:** JSON errors hide internal SQL details; lock contention
  returns 503 and protected maintainer deletion returns 409.
- **Loopback default:** Avoids exposing an unauthenticated service by default.

These safeguards do not make the service production-ready. Authentication,
authorization, TLS, rate limiting, monitoring, backups, and high availability
are not implemented.

## Key design decisions

| Decision | Choice and rationale |
| --- | --- |
| Language | Go provides static typing, standard HTTP/JSON and testing libraries, and straightforward deployment as a compiled executable. |
| HTTP | Standard-library routing and JSON; no web framework needed for this scope. |
| Storage | SQLite with explicit SQL, avoiding a separate database service. |
| Relationship | One maintainer per application, enforced by a non-null foreign key. |
| Updates | PUT for editable fields; PATCH and release history are deferred. |

### Storage guarantees and scaling

- **Consistency:** Each write is atomic. Foreign keys enforce creator existence
  and prevent deleting maintainers who still own applications.
- **Durability:** A local SQLite file uses full synchronization. Committed data
  is intended to survive restarts/crashes, subject to filesystem and hardware
  guarantees; backups are not implemented.
- **Concurrency and scaling:** One pooled connection serializes database access;
  updates use last-successful-write-wins. This targets a small, single-instance
  service. Sustained concurrent writes or multi-host deployment would motivate
  evaluating PostgreSQL.

See the [design document](docs/design.md) for architecture, schema, connection
settings, and tradeoffs. Schema initialization preserves existing data; versioned
migrations are not implemented.

## Testing and GitHub Actions

Run all tests, static checks, and generate coverage:

```powershell
go test -coverprofile coverage.out ./...
go vet ./...
go tool cover -html coverage.out -o coverage.html
```

Open `coverage.html` in a browser. Unit/storage tests cover CRUD, validation,
constraints, lock contention, error mapping, and lifecycle behavior.
The E2E suite uses real compiled-server HTTP requests and temporary SQLite files
to check happy/failure paths and persistence after a forced restart. It cleans up
automatically and includes server logs on failure.

Run only E2E tests with `go test -v ./tests/e2e`; Go must be on `PATH`.
The subprocess is not included in the parent test's coverage report.
Race detection (`go test -race ./...`) requires a supported C toolchain.
The [CI workflow](.github/workflows/ci.yml) runs formatting, vet, build, and all
tests with race detection on Linux for main pushes and pull requests, or manually.
It uploads coverage reports and test logs. Its first hosted run is still pending.

## Cost and time spent

- **Time budget:** Approximately eight hours of focused project work.
- **Actual time spent:** To be recorded as work proceeds, distinguishing design,
  implementation, testing, and documentation.

## Additional notes

- This is a take-home assignment; completeness and understandable code take
  priority over extra features.
- AI assistance is being used for planning, documentation, implementation, and
  testing.
