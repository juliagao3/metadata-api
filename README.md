# Metadata API

A RESTful HTTP API for managing Applications and the Maintainers who created them.

**Status:** Server foundation implemented. The server opens SQLite and exposes
readiness; Application/Maintainer CRUD, the resource schema, E2E suite, and
GitHub Actions are not implemented yet.

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

The database's parent directory must already exist. Startup fails explicitly if
the database cannot be opened or the address cannot be bound. The database file
is created automatically, but resource tables will be added in a later task.

Build a standalone Windows executable:

```powershell
go build -o .\bin\metadata-api.exe .\cmd\server
.\bin\metadata-api.exe
```

Run the current unit, storage, and HTTP lifecycle tests and generate coverage:

```powershell
go test -coverprofile coverage.out ./...
go tool cover -html coverage.out -o coverage.html
```

Open `coverage.html` in a browser. For static checks, run `go vet ./...`.
The commands above target PowerShell on Windows; on Unix-like systems, use
`./cmd/server` and omit the `.exe` extension when building.

## API reference

The [OpenAPI reference](docs/openapi.yaml) is the source of truth for endpoints,
schemas, validation, example requests and responses, update and deletion
semantics, and error codes. The API uses JSON and the `/api/v1` prefix.

The specification can be viewed in any OpenAPI 3.0-compatible viewer. Only
`GET /readyz` is implemented so far; resource endpoints describe intended
behavior and currently return `404`.

## Production features

The foundation includes:

- **Database-backed readiness:** A bounded connectivity check helps callers
  distinguish a running process from one that can access storage.
- **HTTP timeouts:** Header/read/write/idle deadlines limit stalled connections.
- **Graceful shutdown:** Interrupt/termination signals stop new connections and
  allow active requests to finish before storage closes.
- **Explicit failures:** Startup errors are logged and produce a nonzero exit;
  HTTP errors use JSON without exposing database details.
- **Local-only default listener:** Binding to loopback avoids unintentionally
  exposing an unauthenticated development service.

Resource validation, parameterized CRUD queries, resource constraints, and body
limits will be added with the resource endpoints. These safeguards alone do not
make the service production-ready.

Authentication, authorization, TLS termination, rate limiting, monitoring,
automated backups, and high availability are outside the initial scope unless
time and requirements justify them.

## Key design decisions

Go, standard HTTP routing, and SQLite connection setup are implemented.
Resource-specific choices remain the target for subsequent tasks.

| Decision | Choice and rationale |
| --- | --- |
| Language | Go provides static typing, standard HTTP/JSON and testing libraries, and straightforward deployment as a compiled executable. |
| HTTP | Standard-library routing and JSON; no web framework needed for this scope. |
| Storage | SQLite with explicit SQL, avoiding a separate database service. |
| Relationship | One maintainer per application, enforced by a non-null foreign key. |
| Updates | PUT for editable fields; PATCH and release history are deferred. |
| Scope | Complete CRUD, persistence, tests, and documentation before optional features. |

### Storage guarantees and scaling

- **Consistency:** Use SQLite transactions and database constraints. Enable
  foreign keys on every connection. Return success only after the write commits.
- **Durability:** Persist to a local database file and use full synchronization.
  Committed data is intended to survive restarts and crashes, subject to the
  underlying filesystem and hardware honoring synchronization. This does not
  protect against disk loss or replace backups.
- **Concurrency:** Start with one open database connection for a simple,
  serialized access model. Concurrent metadata edits use last-successful-write-wins;
  optimistic concurrency is deferred.
- **Scaling:** Target a small, single-instance service. WAL and additional read
  connections can be evaluated if needed; SQLite still permits only one writer
  at a time. Sustained concurrent writes or multi-host deployment would motivate
  evaluating PostgreSQL.

Current connection settings are one open/idle connection, `foreign_keys=ON`,
`synchronous=FULL`, and a 1,000 ms busy timeout. These settings are reapplied when
the pool opens a replacement connection. New databases use SQLite's default
rollback journal. Readiness has a two-second deadline; database startup has a
five-second deadline. Resource transaction behavior is not implemented yet.

## Testing and GitHub Actions

Current tests cover:

- Readiness responses, routing errors, method handling, and cancellation.
- SQLite settings on initial and replacement connections, foreign-key
  enforcement using test-only tables, file-path escaping, and persistence after
  reopening a database.
- Startup failures and HTTP shutdown waiting for an active request.

Remaining test work includes:

- Unit tests for validation, handler behavior, and error mapping.
- Storage integration tests against temporary SQLite databases.
- End-to-end tests against a compiled, running server over HTTP, including
  persistence across a server restart.
- GitHub Actions checks for formatting, static analysis, build, tests, and race
  detection, with coverage reports and failure logs.

The E2E suite and GitHub Actions workflow are not implemented yet. Local coverage
commands are documented above; results combine the current unit and storage
integration tests. Race detection requires a supported C toolchain even though
the server itself does not.

## Cost and time spent

- **Time budget:** Approximately eight hours of focused project work.
- **Actual time spent:** To be recorded as work proceeds, distinguishing design,
  implementation, testing, and documentation.
- **Current work:** Requirements discussion, README scaffolding, API contract,
  and the initial server foundation with tests. CRUD implementation has not
  started.

## Additional notes

- This is a take-home assignment; completeness and understandable code take
  priority over extra features.
- AI assistance is being used for planning, documentation, implementation, and
  testing.
- Known limitations and any unfinished work will be recorded here before
  submission.
