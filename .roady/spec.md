# Nous Product Specification

## Overview

Nous is a Go-based AI coordination engine that acts as a proactive memory and commitment tracker. It reads conversational or textual input, extracts implied promises and obligations, evaluates the risk that they will be missed, and decides whether to nudge the user, escalate, or automate a remediation action.

Nous lives in the broader ecosystem alongside:
- **Mnemos** — memory / event store (Nous reads memories and appends evidence to it)
- **Chronos** — temporal pattern / signal detection service
- **Praxis** — execution / capabilities layer (Nous decides; Praxis executes)

## Core Purpose

Nous bridges the gap between "something was said that implies future action" and "doing something about it before it's forgotten." It is an intelligent reminder and action-orchestration layer with explainable decision-making.

## Architecture Principles

- **Clean Architecture / Ports & Adapters**: Domain is pure. I/O, SQL, gRPC, LLM SDKs live in outer packages.
- **Interface Segregation**: Per-aggregate repository interfaces; backends implement only what they support.
- **Deterministic Core**: Risk and intervention engines are pure functions (clock injected). Testable without mocks.
- **Audit by Default**: Every extraction and evaluation writes a Decision. Interventions carry full provenance.
- **Fail-Soft**: Chronos/Mnemos failures don't block evaluation. Per-commitment persistence errors are logged and the loop continues.
- **Effect Gating**: Production uses `AxiKernel` (via `axi-go`) so write-external actions require approval, budgets, and leave tamper-evident evidence.

## Key Domain Concepts

### Commitment
A promise, obligation, or implied future responsibility extracted from text. Has:
- `Confidence` — how sure Nous is that this commitment is real (set by LLM extractor)
- `RiskScore` — how likely it is to be missed (maintained by risk engine)
- `DueAt` — optional deadline
- Status lifecycle: `pending` → `in_progress` → (`completed` | `missed` | `cancelled`)

### Decision
An append-only audit record of "Nous chose X because Y." Every non-trivial pipeline branch writes one. Decisions are immutable — corrections come via follow-up decisions.

### Intervention
A proactive message or action suggestion triggered when risk exceeds a threshold:
- **Nudge** — informational reminder
- **Suggestion** — offers a concrete action
- **Escalation** — routes to a different actor
- **Automation** — bypasses user (gated by Praxis/axi-go effect profiles)

### Goal / Plan / Task
A future-facing execution hierarchy. Goals are high-level objectives. Plans decompose goals into ordered steps. Tasks are concrete units of work assignable to agents.

## Features

### Feature: Domain Model Foundation
The pure domain layer with validation, state machines, and invariants. No I/O.

**Requirements:**
- Commitment lifecycle with status transitions and validation
- Decision append-only records with inputs and outcomes
- Intervention types (nudge, suggestion, escalation, automation) and status machine
- Goal, Plan, PlanStep, Task, Assignment types with full lifecycle
- Shared types: Priority, SourceRef, EntityRef, MemoryRef, ChronosSignal
- Domain errors: validation, invalid transitions

### Feature: Ports & Interfaces
Outbound contracts the engine drives. Enables swapping backends and external clients.

**Requirements:**
- Per-aggregate repository interfaces (Commitment, Decision, Intervention, Goal, Task)
- Structured filter types for each repository
- MnemosClient port (Recall, AppendEvent)
- ChronosClient port (GetSignals)
- PraxisClient port (ListCapabilities, DryRun, Execute)
- Transport-safe carrier types (MnemosEvent, Capability, SimulationResult, ExecutionResult)

### Feature: Persistence Layer
Pluggable storage backends with identical behavior.

**Requirements:**
- Memory backend for tests and ephemeral deployments
- SQLite backend with schema migrations
- PostgreSQL backend with schema migrations
- Shared Conn wrapper dispatching on dbType
- Encoding/decoding helpers for JSON fields
- Connection management and cleanup
- Store-level integration tests verifying parity across backends

### Feature: Risk Engine
Pure, deterministic scorer for commitment miss-likelihood.

**Requirements:**
- Configurable additive scoring formula (overdue, due-soon, confidence, chronos signals)
- Score clamped to [0,1]
- Factor tracking for explainability
- Stateless engine safe for concurrent use
- Unit tests for all score combinations

### Feature: Intervention Engine
Policy engine deciding whether to interrupt the user.

**Requirements:**
- Configurable thresholds (nudge, escalate, automation)
- Returns draft intervention or nil based on risk assessment
- Human-readable messages with top contributing factor
- Pure function design for testability

### Feature: Extraction Pipeline
Converts text-with-context into persisted commitments.

**Requirements:**
- Extractor workflow struct with explicit dependency wiring
- LLM CommitmentExtractor port (scripted implementation for tests)
- Confidence threshold filtering
- Dropped draft tracking in Decision audit record
- Error handling: extractor failure is fatal, persistence failure is loud but non-fatal

### Feature: Evaluation Pipeline
The central worker loop: load active commitments, score risk, persist updates, emit interventions.

**Requirements:**
- Evaluator workflow with explicit dependency wiring
- Fetch Mnemos memories and Chronos signals per commitment (soft-fail)
- Update risk scores in repository
- Record one Decision per commitment per pass
- Ask intervention engine; persist if fires
- Limit option for batch sizing
- Continue on per-commitment errors

### Feature: Coordination Layer
The Decision → Action boundary.

**Requirements:**
- Kernel interface: Execute(intervention) → Outcome
- Outcome with evidence records for Mnemos forwarding
- DirectKernel: thin passthrough to Praxis
- AxiKernel: wraps in axi-go for effect-gated approval, budgets, tamper-evident evidence
- ErrNoSuggestedAction for informational interventions

### Feature: LLM Integration
Abstraction over LLM providers for commitment extraction and planning.

**Requirements:**
- CommitmentExtractor interface
- Scripted extractor implementation (deterministic, for tests)
- ExtractInput with OwnerID, Text, Hints, SourceRefs
- Planner interface (future-facing)
- ContextPacket for planner evidence

### Feature: gRPC / HTTP Transport
External API surface for Nous.

**Requirements:**
- gRPC service definitions for Extract, Evaluate, ListCommitments, GetDecision, ListInterventions
- HTTP gateway for REST compatibility
- Request/response DTOs with validation
- Middleware: logging, tracing, auth

### Feature: Worker / Scheduler
Background processing for continuous evaluation.

**Requirements:**
- Configurable tick interval for evaluation loop
- Graceful shutdown handling
- Metrics emission (evaluated, updated, interventions created)
- Single-node MVP; design leaves room for distributed scheduling

### Feature: Observability
Production-grade visibility into Nous operations.

**Requirements:**
- Structured logging with correlation IDs
- OpenTelemetry tracing through pipeline stages
- Metrics: commitments extracted, evaluations run, intervention rate, risk score distribution
- Health check endpoint

### Feature: Configuration & Deployment
Operational concerns for running Nous.

**Requirements:**
- Environment-based configuration (DB type, connection strings, thresholds, API keys)
- 12-Factor App compliance
- Docker / docker-compose setup
- Production readiness checklist

## Non-Goals (for this phase)
- Multi-tenant isolation (assumes single owner or owner-scoped queries)
- Distributed worker scheduling (single-node loop is sufficient)
- Real LLM provider integration (scripted extractor is MVP; real adapter is a future plugin)
- Web UI (Nous is an API service)
- Complex planning workflows (Planner interface declared but not wired)

## Tech Stack
- **Language**: Go 1.26.2
- **Storage**: SQLite (dev), PostgreSQL (prod), memory (tests)
- **External**: Mnemos (memory), Chronos (signals), Praxis (execution)
- **Frameworks**: axi-go (effect gating), bolt (shared library)
- **Observability**: OpenTelemetry

## Success Criteria
- All pipelines have >90% unit test coverage
- All three storage backends pass the same integration test suite
- Evaluation worker runs continuously without memory leaks
- Intervention decisions are fully explainable via Decision audit records
- Effect-gated actions require approval before external writes
