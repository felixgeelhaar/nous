# ADR 004: gRPC Primary with HTTP Gateway

## Status
Accepted

## Context
Nous exposes an API for AI agents and human users. We need:
- Strong typing and schema evolution for machine clients
- Low-latency binary serialization for internal service mesh
- REST endpoints for simple integrations and debugging

## Decision
Use **gRPC as the primary transport** with a **lightweight HTTP REST gateway** alongside it.

- gRPC service defined in `api/nous/v1/nous.proto`
- Go code generated with `protoc-gen-go` and `protoc-gen-go-grpc`
- HTTP server in `internal/transport/http/` provides REST-shaped endpoints
- Both transports share the same application layer (pipelines, repositories)

## Consequences

### Positive
- Strongly typed contracts with automatic client generation
- Bidirectional streaming available for future features
- HTTP gateway allows curl-based debugging without gRPC tools
- OpenAPI spec generated from HTTP endpoints for documentation

### Negative
- Two transports to maintain and test
- HTTP gateway lacks gRPC features (streaming, metadata)
- Protobuf tooling required for schema changes

## Alternatives Considered
- **gRPC-Gateway**: Would auto-generate HTTP from protobuf annotations, but adds complexity
- **GraphQL**: Flexible querying but adds complexity for an MVP
- **REST only**: Simpler but lacks strong typing and streaming

## Notes
The HTTP server is intentionally minimal — it proxies to the same application methods rather than duplicating logic. For production, consider gRPC-Gateway or Connect-RPC for a unified approach.
