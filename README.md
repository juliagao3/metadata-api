# Metadata API

A RESTful HTTP API for managing Applications and the Maintainers who created them.

**Status:** Planning. This repository currently contains only this README. The
server, tests, and CI workflow have not been implemented. Proposed behavior below
will be updated to match the implementation.

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

The proposed API uses JSON and the `/api/v1` prefix.

### Resources

- **Maintainer:** server-generated ID, name, and email.
- **Application:** server-generated ID, title, version, company, website, source,
  license, description, and maintainer ID.

Each application references one existing maintainer. A maintainer can have
multiple applications. Deleting a maintainer with applications will be rejected
rather than silently deleting those applications.

### Proposed endpoints

| Method | Path | Operation | Success |
| --- | --- | --- | --- |
| POST | `/api/v1/maintainers` | Create a maintainer | `201 Created` |
| GET | `/api/v1/maintainers` | List maintainers | `200 OK` |
| GET | `/api/v1/maintainers/{id}` | Retrieve a maintainer | `200 OK` |
| PUT | `/api/v1/maintainers/{id}` | Replace editable maintainer fields | `200 OK` |
| DELETE | `/api/v1/maintainers/{id}` | Delete a maintainer | `204 No Content` |
| POST | `/api/v1/applications` | Create an application | `201 Created` |
| GET | `/api/v1/applications` | List applications | `200 OK` |
| GET | `/api/v1/applications/{id}` | Retrieve an application | `200 OK` |
| PUT | `/api/v1/applications/{id}` | Replace editable application fields | `200 OK` |
| DELETE | `/api/v1/applications/{id}` | Delete an application | `204 No Content` |

### Example request and response

Illustrative only; these examples are not yet backed by a running server.

```http
POST /api/v1/maintainers
Content-Type: application/json

{
  "name": "Alex",
  "email": "alex@example.com"
}
```

```http
HTTP/1.1 201 Created
Location: /api/v1/maintainers/1
Content-Type: application/json

{
  "id": 1,
  "name": "Alex",
  "email": "alex@example.com"
}
```

Complete, verified request and response examples for both resources, including
update semantics and list response formats, will be added with the implementation.

### Proposed error codes

| Status | Meaning |
| --- | --- |
| `400 Bad Request` | Malformed JSON, invalid path IDs, or invalid query parameters |
| `404 Not Found` | Requested resource does not exist |
| `405 Method Not Allowed` | Method is not supported for the requested path |
| `409 Conflict` | Maintainer cannot be deleted while applications reference it |
| `413 Content Too Large` | Request body exceeds the configured limit |
| `415 Unsupported Media Type` | Request body is not JSON |
| `422 Unprocessable Content` | Field validation fails or a referenced maintainer does not exist |
| `503 Service Unavailable` | Database remains busy after a bounded wait |
| `500 Internal Server Error` | Unexpected server or storage failure |

Proposed error format:

```json
{
  "error": {
    "code": "validation_failed",
    "message": "Request validation failed.",
    "fields": {
      "title": "must not be blank"
    }
  }
}
```

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
| Language | Go, matching the team's stack and providing standard HTTP and testing libraries. |
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
- **Current work:** Initial requirements discussion and README scaffolding only;
  server implementation has not started.

## Additional notes

- This is a take-home assignment; completeness and understandable code take
  priority over extra features.
- AI assistance is being used for planning and documentation. Its use in
  implementation and testing will be documented if applicable.
- Known limitations and any unfinished work will be recorded here before
  submission.
