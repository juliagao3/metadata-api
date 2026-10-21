# Metadata API

A RESTful HTTP API for managing Applications and the Maintainers who created them.

**Status:** API contract defined for review. The server, tests, and CI workflow
have not been implemented. Documentation describes intended behavior, not a
running service.

## Quick start

Build, run, and test commands will be added once the server is implemented and
verified from a clean checkout.

| Task | Instructions |
| --- | --- |
| Prerequisites | Planned: Go and Git; exact Go version to be documented. |
| Build | To be added. |
| Run | To be added, including listen address and SQLite database location. |
| Test | To be added, covering unit, storage integration, and end-to-end tests. |
| Coverage | To be added, including local report generation. |

The goal is a short local setup without a separate database service.

## API reference

The [OpenAPI reference](docs/openapi.yaml) is the source of truth for endpoints,
schemas, validation, example requests and responses, update and deletion
semantics, and error codes. The API uses JSON and the `/api/v1` prefix.

The specification can be viewed in any OpenAPI 3.0-compatible viewer. It
describes intended behavior; it has not yet been verified against a server.

## Production features

**None implemented yet.** This section will describe only features actually
built and verified, with the reason for each.

Planned baseline safeguards include input validation, parameterized SQL, database
constraints, bounded request bodies, consistent errors, and a readiness endpoint
for automated tests. These safeguards alone do not make the service
production-ready.

Authentication, authorization, TLS termination, rate limiting, monitoring,
automated backups, and high availability are outside the initial scope unless
time and requirements justify them.

## Key design decisions

The following are proposed choices, not completed implementation claims:

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

Final connection settings and transaction behavior will be documented after
implementation.

## Testing and GitHub Actions

The planned test strategy includes:

- Unit tests for validation, handler behavior, and error mapping.
- Storage integration tests against temporary SQLite databases.
- End-to-end tests against a compiled, running server over HTTP, including
  persistence across a server restart.
- GitHub Actions checks for formatting, static analysis, build, tests, and race
  detection, with coverage reports and failure logs.

No tests, coverage results, or workflow runs exist yet. Commands, workflow links,
and report locations will be added once available.

## Cost and time spent

- **Time budget:** Approximately eight hours of focused project work.
- **Actual time spent:** To be recorded as work proceeds, distinguishing design,
  implementation, testing, and documentation.
- **Current work:** Requirements discussion, README scaffolding, and API contract
  definition; server implementation has not started.

## Additional notes

- This is a take-home assignment; completeness and understandable code take
  priority over extra features.
- AI assistance is being used for planning and documentation. Its use in
  implementation and testing will be documented if applicable.
- Known limitations and any unfinished work will be recorded here before
  submission.
