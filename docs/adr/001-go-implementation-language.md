# ADR 001: Go as Implementation Language

## Status
Accepted

## Context
Nous is an AI coordination engine that needs to be:
- Fast and resource-efficient (runs alongside AI agents)
- Deployable as a single binary with minimal dependencies
- Easy to operate with strong concurrency primitives

We considered Python (ecosystem familiarity for AI), Rust (performance), TypeScript/Node (team familiarity), and Go.

## Decision
Use Go 1.26+ as the primary implementation language.

## Consequences

### Positive
- Single static binary deployment with CGO-disabled builds
- Excellent concurrency with goroutines and channels
- Strong standard library (net/http, database/sql, context)
- Fast compile times enable rapid iteration
- Built-in race detector and profiling tools
- gRPC and protobuf first-class support

### Negative
- Less AI/ML ecosystem than Python (mitigated by keeping LLM integration thin)
- More boilerplate than Python or TypeScript
- No generics until Go 1.18 (not relevant for Go 1.26+)

## Alternatives Considered
- **Python**: Better AI ecosystem but harder to deploy as a single binary, GIL limits concurrency
- **Rust**: Better performance guarantees but slower development velocity, steeper learning curve
- **TypeScript/Node**: Good async model but runtime overhead and larger deployment artifacts
