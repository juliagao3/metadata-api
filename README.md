# Metadata API

A RESTful HTTP API for managing Applications and the Maintainers who created them.

**Status:** Server foundation, resource schema, and Maintainer create/read/list/update
are implemented. Maintainer delete, Application CRUD, the E2E suite, and
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

Create a maintainer from another PowerShell terminal:

```powershell
$body = @{ name = "Alex"; email = "alex@example.com" } | ConvertTo-Json
Invoke-RestMethod -Uri http://127.0.0.1:8080/api/v1/maintainers -Method Post -ContentType application/json -Body $body
```

The server returns `201 Created`, a `Location` header such as
`/api/v1/maintainers/1`, and the saved maintainer with its generated ID. Names and
emails are preserved as supplied, and duplicate names/emails are allowed.

Retrieve the created maintainer using its returned ID, or list all maintainers:

```powershell
Invoke-RestMethod -Uri http://127.0.0.1:8080/api/v1/maintainers/1
Invoke-RestMethod -Uri http://127.0.0.1:8080/api/v1/maintainers
```

Both return `200 OK`. A missing maintainer returns `404`. Lists are ordered by
ascending ID, return `[]` when empty, and are not paginated in this version.

Replace a maintainer's name and email using its existing ID:

```powershell
$body = @{ name = "Alex Updated"; email = "updated@example.com" } | ConvertTo-Json
Invoke-RestMethod -Uri http://127.0.0.1:8080/api/v1/maintainers/1 -Method Put -ContentType application/json -Body $body
```

PUT requires both fields and returns `200 OK` with the saved record and unchanged
ID. It does not create missing resources (`404`) or accept client-supplied IDs.

Optional configuration:

```powershell
go run .\cmd\server -addr 127.0.0.1:8081 -db .\metadata.db
```

The database's parent directory must already exist. Startup fails explicitly if
the database cannot be opened, schema initialization fails, or the address
cannot be bound. The database file and resource tables are created automatically.

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

The specification can be viewed in any OpenAPI 3.0-compatible viewer.
`GET /readyz`, `POST /api/v1/maintainers`, `GET /api/v1/maintainers`,
`GET /api/v1/maintainers/{id}`, and `PUT /api/v1/maintainers/{id}` are implemented.
GET routes also support HEAD.
Unsupported methods on these paths return `405` with an `Allow` header.
Application paths currently return `404`; their contracts describe intended
behavior.

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
- **Transactional schema initialization:** Tables and the relationship index are
  initialized together before HTTP startup, without clearing existing data.
- **Relational integrity:** Non-null foreign keys prevent orphaned applications,
  and restricted deletion protects a maintainer's existing applications.
- **Bounded, validated Maintainer requests:** Creation/update accept one JSON
  object up to 64 KiB, reject unknown fields/nulls/incorrect types, and validate
  required fields, name/email lengths, and email syntax.
- **Parameterized writes:** Maintainer values are passed separately from SQL.
  SQLite assigns IDs on creation and preserves them on update; lock contention
  is reported as `503`, while unexpected storage failures return `500`.

Equivalent validation and storage operations for the remaining endpoints will
be added with those endpoints. These safeguards alone do not make the service
production-ready.

Authentication, authorization, TLS termination, rate limiting, monitoring,
automated backups, and high availability are outside the initial scope unless
time and requirements justify them.

## Key design decisions

Go, standard HTTP routing, SQLite connection setup, the resource schema, and
Maintainer create/read/list/update are implemented. Remaining resource endpoints
are subsequent tasks.

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
five-second deadline, including schema initialization. Maintainer creation and
update each use one atomic statement (`INSERT/UPDATE ... RETURNING`) with SQLite's
implicit transaction; neither requires a separate multi-statement transaction.

### Database schema

There are two resource tables: `maintainers` and `applications`, with one row per
resource. The initial schema is in
[`internal/storage/schema.sql`](internal/storage/schema.sql), embedded into the
executable so it does not depend on a SQL file at runtime.

Both tables use SQLite STRICT typing, positive integer primary keys, and
non-null fields. Optional application strings default to `""`.
`applications.maintainer_id` references `maintainers.id` with `ON DELETE RESTRICT`
and has an index for relationship lookups.

Startup applies `CREATE TABLE/INDEX IF NOT EXISTS` within a transaction. Reopening
a database preserves existing records. This initializes a fresh schema; it does
not migrate or fully validate an arbitrary existing schema. Future structural
changes will require explicit migrations. Maintainer creation/update validate email
syntax, nonblank names, and field-length limits in the HTTP layer. Application
field validation will be added with its endpoints.

## Testing and GitHub Actions

Current tests cover:

- Readiness responses, routing errors, method handling, and cancellation.
- SQLite settings on initial and replacement connections, file-path escaping,
  and resource persistence after reopening a database.
- Both resource tables, optional-field defaults, required fields, foreign keys,
  restricted deletion, and rollback on schema initialization failure.
- Startup failures and HTTP shutdown waiting for an active request.
- Maintainer creation responses and input validation, request size limits,
  content types, and storage-error mapping.
- Parameterized Maintainer insertion, duplicate metadata, persistence,
  cancellation, and recovery after database lock contention.
- Maintainer retrieval/listing, ascending ID order, empty arrays, strict path-ID
  parsing, missing resources, HEAD responses, and read-failure handling.
- Maintainer replacement, required PUT fields, stable IDs, repeated identical
  updates, no upsert, cancellation, and preservation of unrelated records.

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
  server foundation, resource schema, and Maintainer create/read/list/update with tests.
  Remaining CRUD operations have not been implemented.

## Additional notes

- This is a take-home assignment; completeness and understandable code take
  priority over extra features.
- AI assistance is being used for planning, documentation, implementation, and
  testing.
- Known limitations and any unfinished work will be recorded here before
  submission.
