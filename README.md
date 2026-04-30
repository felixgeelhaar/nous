# Nous

Nous is an AI coordination engine that extracts commitments from natural language, evaluates their risk of being missed, and intervenes when necessary. It acts as the "nervous system" between AI agents and human intent — tracking what was promised, scoring the likelihood of delivery, and surfacing nudges or escalations before deadlines slip.

## Features

- **Commitment Extraction** — Parses text (conversations, emails, notes) into structured commitments with confidence scores.
- **Risk Evaluation** — Continuously scores active commitments using time-to-deadline, confidence, and external signals.
- **Intervention Engine** — Automatically creates nudges, suggestions, escalations, or automation requests when risk crosses thresholds.
- **Multi-Backend Storage** — Supports in-memory, SQLite, and PostgreSQL backends with identical behavior.
- **gRPC + HTTP APIs** — Primary gRPC API with a lightweight REST gateway for easy integration.
- **Observability** — Structured JSON logging with correlation IDs, OpenTelemetry tracing, and Prometheus-style metrics.

## Quick Start

### Prerequisites

- Go 1.26+
- (Optional) PostgreSQL 15+ for production deployments

### Run Locally

```bash
# SQLite backend (default)
go run ./cmd/nous

# With explicit config
NOUS_DB_TYPE=sqlite NOUS_DB_DSN=nous.db NOUS_GRPC_ADDR=:50051 NOUS_HTTP_ADDR=:8080 go run ./cmd/nous

# PostgreSQL backend
NOUS_DB_TYPE=postgres NOUS_DB_DSN=postgres://user:pass@localhost/nous?sslmode=disable go run ./cmd/nous
```

### Docker

```bash
docker build -t nous:latest .
docker run -p 50051:50051 -p 8080:8080 \
  -e NOUS_DB_TYPE=sqlite \
  -e NOUS_DB_DSN=/data/nous.db \
  -e NOUS_HTTP_ADDR=:8080 \
  -v nous-data:/data \
  nous:latest
```

Or use Docker Compose:

```bash
docker-compose up
```

## Configuration

All configuration is via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `NOUS_GRPC_ADDR` | `:50051` | gRPC listen address |
| `NOUS_HTTP_ADDR` | *(empty)* | HTTP listen address (disabled if empty) |
| `NOUS_DB_TYPE` | `sqlite` | Database backend: `memory`, `sqlite`, `postgres` |
| `NOUS_DB_DSN` | `nous.db` | Connection string or file path |
| `NOUS_TICK_INTERVAL` | `5m` | Evaluation worker tick interval |
| `NOUS_EXTRACT_MIN_CONFIDENCE` | `0.0` | Minimum confidence to keep extracted commitments |
| `NOUS_RISK_OVERDUE_WEIGHT` | `0.6` | Risk weight for overdue commitments |
| `NOUS_RISK_DUE_SOON_WEIGHT` | `0.3` | Risk weight for near-deadline commitments |
| `NOUS_RISK_DUE_SOON_WINDOW` | `2h` | Window considered "due soon" |
| `NOUS_RISK_CONFIDENCE_WEIGHT` | `0.2` | Risk weight for low confidence |
| `NOUS_NUDGE_THRESHOLD` | `0.5` | Risk score to trigger a nudge |
| `NOUS_ESCALATE_THRESHOLD` | `0.85` | Risk score to trigger an escalation |
| `NOUS_AUTOMATION_CONFIDENCE` | `0.95` | Minimum confidence for automation suggestions |

## API

### gRPC

The gRPC service is defined in `api/nous/v1/nous.proto`. Methods include:

- `Extract` — Extract commitments from text
- `Evaluate` — Run risk evaluation on active commitments
- `ListCommitments` / `GetCommitment` — Query commitments
- `ListInterventions` / `GetIntervention` — Query interventions
- `ResolveIntervention` — Accept, reject, or execute an intervention

### HTTP REST

When `NOUS_HTTP_ADDR` is set, the following REST endpoints are available:

| Method | Path | Description |
|--------|------|-------------|
| POST | `/v1/extract` | Extract commitments from text |
| POST | `/v1/evaluate` | Trigger evaluation loop |
| GET | `/v1/commitments` | List commitments |
| GET | `/v1/commitments/{id}` | Get a commitment |
| GET | `/v1/interventions` | List interventions |
| POST | `/v1/interventions/{id}/resolve` | Resolve an intervention |
| GET | `/v1/health` | Health/readiness check |
| GET | `/metrics` | Prometheus-style metrics snapshot |

## Architecture

```
cmd/nous              # Application entrypoint
api/nous/v1           # Protobuf service definitions
internal/
  domain/             # Pure domain types (commitment, intervention, decision)
  ports/              # Repository and client interfaces
  store/              # Persistence dispatch + memory/sqlite/postgres backends
  pipeline/           # Extract and Evaluate workflows
  risk/               # Risk scoring engine
  intervention/       # Intervention policy engine
  coordination/       # Direct and Axi (axi-go) kernels
  llm/                # LLM extractors (scripted MVP, pluggable)
  transport/
    grpc/             # gRPC server implementation
    http/             # REST gateway
  worker/             # Background evaluation scheduler
  observability/      # Logging, tracing, metrics, middleware
  config/             # Environment-based configuration
```

## Testing

```bash
# Run all tests
go test ./...

# Run with PostgreSQL (requires running Postgres)
NOUS_TEST_POSTGRES_DSN=postgres://user:pass@localhost/nous_test?sslmode=disable go test ./internal/store/...

# Run end-to-end smoke test
go test ./e2e/... -v
```

## Production Readiness

- **12-Factor App** — Config via environment, stateless processes, port binding, disposability
- **Graceful Shutdown** — SIGTERM drains in-flight gRPC/HTTP requests and stops the worker
- **Health Checks** — `/v1/health` includes readiness probe with DB connectivity check
- **Structured Logging** — JSON logs to stdout with correlation IDs
- **Tracing** — OpenTelemetry spans for Extract and Evaluate pipelines
- **Metrics** — Atomic counters for commitments extracted, evaluations run, interventions created, risk distribution
- **Security** — Distroless non-root container, no secrets in code

## License

MIT
