# Metadata API design

This document describes the implemented server, rather than the original
proposal. The [README](../README.md) covers setup and examples; the
[OpenAPI specification](openapi.yaml) defines the detailed HTTP contract.

## Scope and technology choices

The goal is complete, understandable CRUD for Applications and their creator
Maintainers, with persistent storage and predictable failures.

- **Go and `net/http`:** Typed models, routing, JSON, contexts, and testing are
  available without a web framework. A compiled executable is straightforward to
  run locally.
- **SQLite through `database/sql`:** Relational constraints and transactions
  provide durable storage without provisioning an external database server.
  Explicit SQL keeps the small set of queries visible instead of adding an ORM.
- **Pure-Go SQLite driver:** `modernc.org/sqlite` avoids requiring a C compiler
  to build the server. Go's race detector still requires a supported C toolchain.
- **JSON only:** One request/response representation keeps implementation and
  validation focused. YAML, PATCH, search, and pagination are outside this version.

PostgreSQL would suit a larger multi-instance deployment, but would add setup and
operational work that this single-instance assignment does not need.

## Architecture and request flow

```text
HTTP client
    |
    v
net/http router -> JSON decoding and validation
    |
    v
storage.Store -> database/sql -> SQLite file
```

| Component | Responsibility |
| --- | --- |
| [`cmd/server`](../cmd/server) | Flags, database startup, HTTP listener, timeouts, and shutdown. |
| [`internal/api`](../internal/api) | Routing, input validation, response headers, JSON responses, and public error mapping. |
| [`internal/storage`](../internal/storage) | Models, embedded schema, connection settings, parameterized SQL, and storage error classification. |
| [`tests/e2e`](../tests/e2e) | Tests through HTTP against a separately compiled server and temporary database. |

Startup opens SQLite, applies the embedded schema transactionally, and only then
starts listening. Handlers call a small database interface; production uses
`storage.Store`, while handler unit tests can substitute failures without running
a server. Storage does not depend on HTTP.

A write passes JSON and domain validation before reaching storage. The store
executes one parameterized `INSERT`, `UPDATE`, or `DELETE ... RETURNING` statement.
Its result determines the response; there is no separate read-before-write
existence check. This avoids races between checking a record and modifying it.

## Resources and ownership

The [schema](../internal/storage/schema.sql) contains two STRICT tables:

- `maintainers`: a positive, server-assigned integer ID, name, and email.
- `applications`: a positive, server-assigned integer ID, the seven metadata
  strings, and a required `maintainer_id`.

One maintainer can create many applications; each application has exactly one
creator. A non-null foreign key enforces that the creator exists.
`ON DELETE RESTRICT` prevents deleting a maintainer while applications reference
them. An index on `applications.maintainer_id` supports relationship checks.
Deleting an application never deletes its maintainer or another application.

The API does not allow changing resource IDs or an application's creator.
Names, emails, titles, and versions are not unique: the requirements do not
establish those fields as identities. Application responses carry the creator's
ID rather than embedding a second resource.

All stored fields are non-null, and optional application strings default to
`""`. Required text, email syntax, and Unicode string-length limits are validated
in the HTTP layer, not duplicated as SQL string-validation constraints.
Version, website, source, and license are metadata strings, without SemVer,
URL-format, or license-name restrictions.

## API behavior decisions

POST creates resources and returns their generated IDs and a relative Location
header. GET retrieves a resource or a plain array ordered by ID; empty collections
are `[]`. GET routes also support HEAD without a response body.

PUT replaces editable fields, preserves identity and ownership, and never
upserts. Required fields must be supplied again; omitted optional application
metadata is cleared to `""`. Repeating the same PUT has the same intended effect.
DELETE returns an empty 204 response; missing or previously deleted resources
return 404.

Write requests must contain exactly one JSON object within 64 KiB. Unknown or
incorrectly capitalized fields, null values, incorrect types, and invalid UTF-8
are rejected. IDs must be positive signed 64-bit integers in decimal digits.
Query parameters are rejected rather than silently ignored.

Errors use a consistent JSON envelope. Invalid request structure is distinct
from domain validation failure, including field-specific errors for a missing
creator. Protected maintainer deletion returns 409. Busy/locked SQLite failures
map to 503; unexpected storage failures are logged and return a generic 500
without exposing SQL details. Full status codes and schemas live in OpenAPI.

## Storage guarantees

**Consistency:** Each mutation is one atomic statement with SQLite's implicit
transaction; success is returned only after it completes. Foreign keys are
enabled on every connection, including replacements opened by the pool. SQLite
checks creator existence and deletion restrictions as part of the write, not
through a racy application-level precheck.

**Concurrency:** The pool permits one open/idle connection, serializing database
operations within this process while HTTP requests can arrive concurrently.
Concurrent updates use last-successful-write-wins; there are no ETags or
optimistic concurrency checks. The SQLite busy timeout is 1,000 ms. Lock errors
are surfaced explicitly rather than hidden behind application-level retries.

**Durability:** Data is stored in a local file with `synchronous=FULL`, reapplied
on replacement connections. New databases use SQLite's default rollback journal;
WAL is not enabled. Committed writes are intended to survive process restarts
and crashes, subject to the filesystem and hardware honoring synchronization.
This does not protect against disk loss or replace backups.

**Schema lifecycle:** The SQL schema is embedded in the executable.
`CREATE TABLE/INDEX IF NOT EXISTS` runs inside a startup transaction, preserving
existing rows and rolling back failed initialization. This is a bootstrap
mechanism, not a versioned migration framework or a full validator for arbitrary
existing schemas. Structural changes will require explicit migrations.

## Operational boundaries and scaling

The server defaults to loopback and provides database-backed readiness, bounded
startup/readiness checks, HTTP timeouts, and graceful shutdown. These are useful
safeguards, not a claim of production readiness. Authentication, authorization,
TLS termination, rate limiting, monitoring, backups, and high availability are
not implemented.

This design targets a small, single-instance workload. Whole-collection reads
and one pooled connection limit growth. Pagination and query indexing should be
driven by actual access patterns. WAL and additional read connections could
improve read concurrency, but would not remove SQLite's single-writer limit.
Sustained concurrent writes or multi-host deployment would motivate evaluating
PostgreSQL and revisiting deployment, migrations, and concurrency control.

## Verification and remaining work

Unit tests cover handlers, validation, and error mapping. Storage tests use real
SQLite databases to exercise constraints, persistence, cancellation, and lock
contention. Lifecycle tests cover startup and shutdown behavior.

The retained E2E suite builds the server, sends real HTTP requests, and verifies
CRUD, failures, ownership protection, and saved creates/updates/deletions after
a forced process restart. This demonstrates process-restart persistence, not
power-loss recovery or hardware durability. The separate executable is not
included in the parent test's coverage report.

The [GitHub Actions workflow](../.github/workflows/ci.yml) is prepared for Linux
formatting, static analysis, build, race-enabled tests, and coverage/log artifacts.
Its first hosted run is pending. Race detection covers code executed in the test
processes, not the separate ordinary executable built by the E2E suite.
No load-test or high-availability guarantees are claimed.
