# Contributing to Nous

Thank you for your interest in contributing! This document provides guidelines for participating in the project.

## Development Setup

1. **Prerequisites**
   - Go 1.26+
   - (Optional) PostgreSQL 15+ for integration tests

2. **Clone and Build**
   ```bash
   git clone https://github.com/felixgeelhaar/nous.git
   cd nous
   go build ./...
   ```

3. **Run Tests**
   ```bash
   # All tests
   go test ./...

   # With coverage
   go test ./... -cover

   # With PostgreSQL
   NOUS_TEST_POSTGRES_DSN=postgres://user:pass@localhost/nous_test?sslmode=disable go test ./internal/store/...
   ```

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Run `go mod tidy` before committing
- Add tests for new functionality
- Maintain backwards compatibility for public APIs
- Use meaningful variable names

## Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` — New feature
- `fix:` — Bug fix
- `docs:` — Documentation changes
- `test:` — Test additions or corrections
- `refactor:` — Code refactoring
- `perf:` — Performance improvements
- `chore:` — Maintenance tasks

Example:
```
feat: add HTTP rate limiting middleware

Implements token-bucket rate limiting per client IP
with configurable requests-per-second and burst limits.
```

## Pull Request Process

1. Fork the repository and create a feature branch
2. Ensure all tests pass (`go test ./...`)
3. Update documentation if needed
4. Submit a PR with a clear description of changes
5. Address review feedback promptly

## Architecture Decisions

- **Domain-first**: Business logic lives in `internal/domain/` with no external dependencies
- **Ports and Adapters**: `internal/ports/` defines interfaces; implementations live in subpackages
- **Pipeline pattern**: Cross-aggregate workflows are explicit structs with injected dependencies
- **Storage parity**: All backends (memory, sqlite, postgres) pass the same integration test suite

## Testing Guidelines

- **Unit tests**: Test domain types and pure functions in isolation
- **Integration tests**: Test repository implementations against real backends
- **E2E tests**: Test full request/response cycles through transport layers
- **Benchmarks**: Add benchmarks for performance-critical paths

## Questions?

Open an issue for discussion before large changes. For bugs, include:
- Steps to reproduce
- Expected vs actual behavior
- Environment details (Go version, OS, database backend)
