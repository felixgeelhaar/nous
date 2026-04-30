# ADR 002: Domain-Driven Design with Clean Architecture

## Status
Accepted

## Context
Nous tracks multiple aggregate types (Commitment, Intervention, Decision, Goal, Task) with complex business rules. We need:
- Clear boundaries between business logic and infrastructure
- Testability without real databases or external services
- Ability to swap storage backends without touching domain code

## Decision
Apply Domain-Driven Design (DDD) principles with Clean Architecture layering:

1. **Domain layer** (`internal/domain/`): Pure types, state machines, validation, and business rules. No external dependencies.
2. **Ports layer** (`internal/ports/`): Interface contracts for repositories and external clients.
3. **Application layer** (`internal/pipeline/`, `internal/risk/`, `internal/intervention/`): Orchestration workflows that use domain types and ports.
4. **Infrastructure layer** (`internal/store/`, `internal/transport/`, `internal/llm/`): Concrete implementations of ports.

## Consequences

### Positive
- Domain code is completely framework-agnostic and testable in isolation
- Storage backends (memory, sqlite, postgres) are interchangeable
- Business rules live in one place (domain package) preventing drift
- Test pyramid is natural: unit tests for domain, integration for stores, e2e for transport

### Negative
- More packages and files than a simpler layered architecture
- New developers need to understand DDD concepts (aggregate, repository, port)
- Some boilerplate for interface definitions and mappers

## Alternatives Considered
- **MVC/Three-layer**: Simpler but leads to business logic leaking into controllers
- **Hexagonal Architecture**: Very similar to what we chose; Clean Architecture is a specific flavor
- **Transaction Script**: Would be simpler initially but doesn't scale with complexity
