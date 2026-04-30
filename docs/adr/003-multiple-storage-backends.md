# ADR 003: Multiple Storage Backends with Parity Testing

## Status
Accepted

## Context
Nous needs persistence that works across environments:
- **Development**: Fast iteration with in-memory or SQLite
- **Testing**: Deterministic, isolated tests with in-memory backend
- **Production**: PostgreSQL for durability and concurrent access
- **Demo/Docker**: SQLite for zero-config setup

## Decision
Support three storage backends with identical behavior verified by a shared parity test suite:

1. **Memory** (`internal/store/memory/`): Map-based, copy-on-read semantics, single mutex
2. **SQLite** (`internal/store/sqlite/`): Pure-Go driver (modernc.org/sqlite), WAL mode, embedded migrations
3. **PostgreSQL** (`internal/store/postgres/`): lib/pq driver, jsonb for flexible fields, embedded migrations

The `internal/store/store.go` factory dispatches on `DBType` config. All backends implement the same `ports` interfaces. The `internal/store/store_test.go` parity suite runs the same CRUD and filter tests against all three backends.

## Consequences

### Positive
- One codebase supports all environments without conditional logic in application code
- Parity tests catch backend-specific bugs immediately
- Easy to add new backends (just implement the port interfaces)
- SQLite provides production-grade persistence for small deployments

### Negative
- Three backends to maintain (schema migrations, encoding/decoding)
- PostgreSQL tests require a running database (skipped in CI without env var)
- Some features may not map cleanly across backends (e.g., JSON queries)

## Alternatives Considered
- **PostgreSQL only**: Simpler but requires Docker/database for development and testing
- **SQLite only**: Simpler but not suitable for high-concurrency production
- **ORM (GORM/sqlc)**: Would reduce boilerplate but adds dependency and abstracts too much
