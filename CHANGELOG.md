# Changelog

All notable changes to Nous are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

No unreleased changes.

## [0.3.0] — 2026-05-03

Replayable scoring + MCP transport.

### Added
- **`DecisionWeights` field on `domain.Decision`** — every recorded decision now persists the scoring coefficients alongside Inputs and Outcome. A replay against a newer policy can diff exactly because the weights that produced last week's score are stamped on the row. New `risk.Engine.Weights()` snapshot helper. Migration `002_decision_weights.sql` adds a nullable `weights` column to `decisions` on sqlite + postgres. HTTP `decisionJSON` exposes the field.
- **MCP stdio transport** — new `nous mcp` subcommand. Four tools (`nous_extract`, `nous_evaluate`, `nous_list_interventions`, `nous_list_decisions`) proxy through the running HTTP server. Same pipeline, same audit chain — MCP is a transport, not a parallel code path. Configure via `NOUS_HTTP_URL` (default `http://localhost:8080`) and optional `NOUS_AUTH_TOKEN`.
- **Praxis live HTTP adapter** — `internal/adapters/praxis/grpc.go` rewritten from gRPC stub to real HTTP client speaking Praxis's three-verb REST surface (`/v1/capabilities`, `/v1/actions`, `/v1/actions/{id}/dry-run`). Wire types mirror Praxis's domain JSON shapes locally, so Nous stays decoupled from `praxis/internal/domain`. Bearer auth and TLS-cert pinning remain opt-in. Empty `Addr` still produces a "disabled" adapter (`ErrPraxisDisabled`). Tests: `http_test.go` covers list/execute/dry-run + upstream-error propagation + execute-failure surfacing against an `httptest` fake.
- **Server-side TLS + mTLS** — `NOUS_TLS_CERT_FILE` + `NOUS_TLS_KEY_FILE` enable TLS on HTTP and gRPC simultaneously. `NOUS_MTLS_CLIENT_CA_FILE` upgrades to mutual TLS (clients must present a cert signed by the CA). Helpers: `serverTLSConfig`, `serverTransportCredentials`.
- **Praxis pipeline wiring** — `pipeline.Evaluator` accepts a `ports.PraxisClient`. Automation-class interventions trigger an out-of-band `praxisDryRun` so the audit trail captures predicted side effects. Praxis stays opt-in; `NOUS_PRAXIS_ADDR` empty means no-op.
- **Standalone-mode tolerance** — `/v1/health` only checks Mnemos / Chronos / Praxis adapters when their `NOUS_*_ADDR` is configured. Empty addr = adapter unconfigured = no health impact. Pipeline soft-fails enrichment to nil. Operators can run Nous as a single binary without any cognitive-stack dependencies; wire upstreams later without code changes.
- **Automated JWT key rotation** (`internal/auth/rotator.go`) — background `Rotator` regenerates the active signing key every `NOUS_JWT_ROTATE_PERIOD`, demoting the outgoing key to previous for `NOUS_JWT_ROTATE_OVERLAP`. `Verifier` reads the live snapshot via `NewVerifierFromRotator` so all transports stay coherent without re-wiring.
- **Context-window management** (`internal/llm/context_window.go`) — token-aware head-tail elision via `FitText`. Per-provider budgets (`anthropic`, `openai`, `gemini`, `bedrock`) ship in `DefaultBudgets`; `WithContextBudget` opts the `LLMExtractor` in. Inputs exceeding the available budget collapse to "head[…]tail" instead of triggering provider context-overflow errors.
- **A/B prompt routing** (`internal/llm/ab_prompt.go`) — `ABRouter` deterministically splits traffic across `PromptVariant`s by hashing `OwnerID`. Same owner → same variant for evaluation stability. Hot-swappable via `SetVariants`. `LastVariant()` exposes the last-routed label for stamping run metadata.
- **Golden-output regression harness** (`internal/llm/golden.go`) — `GoldenDataset` + `Run(ctx, extractor, ds)` produces a `Report` with precision / recall / per-case missing / spurious / low-conf annotations. Default dataset (`DefaultGoldenDataset`) ships canonical extraction cases. Operators load custom datasets via `LoadGoldenDataset(path)`.
- **JWT auth** (HS256, `internal/auth/jwt.go`) on HTTP + gRPC alongside the static bearer. Dual-key rotation: `NOUS_JWT_SECRET` (active) + `NOUS_JWT_PREV_SECRET` (previous) accept tokens issued under either, supporting zero-downtime key rotation. Default TTL via `NOUS_JWT_TTL` (1 h).
- **Per-owner rate limiting** (`internal/transport/http/ratelimit_owner.go`) layered above the existing IP-based limiter. Owner key extracted from query (`?owner_id=`), header (`X-Nous-Owner`), or POST body (`{"owner_id":...}`); falls back to client IP. Health/metrics exempt.
- **CEL input validation** (`internal/validation/cel.go`) configurable via `NOUS_VALIDATION_RULES="name=expr,..."`. Variables: `owner_id`, `text`, `text_len`, `source_kinds`. Wired into `POST /v1/extract`.
- **Gemini provider** (`internal/llm/gemini.go`) — Generative Language API with `responseMimeType: application/json`. Default model `gemini-2.5-flash`.
- **Bedrock provider** (`internal/llm/bedrock.go`) — Amazon Bedrock InvokeModel with the Anthropic-on-Bedrock request shape. AWS credentials from the standard credential chain (env, shared config, instance profile, IRSA).
- **Few-shot prompts + versioning** — `PromptBuilder` accepts `WithFewShotExamples(...)` and `WithSystemPrompt(...)`; `PromptVersion = "v2"`; canonical default examples ship.
- **Praxis stub adapter** (`internal/adapters/praxis/grpc.go`) — `ports.PraxisClient` implementation. With no `Addr` configured, returns `ErrPraxisDisabled`. (Superseded above by the live HTTP adapter once Praxis shipped.)
- HTTP `GET /v1/decisions` and `GET /v1/decisions/{id}` endpoints for audit replay (parity with the gRPC surface).
- `internal/llm/anthropic.go` and `internal/llm/openai.go` — real HTTP transport for both providers, base-URL override for OpenAI-compatible endpoints, shared `parseDrafts` helper that strips code fences and trims preambles.
- `internal/transport/http/auth.go` and `internal/transport/grpc/auth.go` — inbound bearer-token middleware/interceptor enforced on `/v1/*` paths (health and metrics stay open). Constant-time compare; empty `NOUS_AUTH_TOKEN` disables auth.
- `config.LLMConfig` (`NOUS_LLM_PROVIDER`, `NOUS_LLM_API_KEY`, `NOUS_LLM_MODEL`, `NOUS_LLM_BASE_URL`) plus `NOUS_AUTH_TOKEN`.
- `SECURITY.md` covering the threat model and `findings.json` baseline policy.
- `docs/SLOs.md` — formal availability, latency, and error-rate SLOs with error-budget burn-rate definitions.
- `docs/DEPLOYMENT.md` — operator runbook.
- `docs/adr/006-llm-extractor-transition.md` — supersedes the "Future Work" section of ADR-005.
- `api/openapi.yaml` — covers `/v1/decisions*` endpoints and `bearerAuth` security scheme.
- `CHANGELOG.md`.

### Changed
- `Dockerfile` base images pinned to specific digests (`golang:1.26-alpine`, `gcr.io/distroless/static-debian12:nonroot`).
- `internal/llm/llm_extractor.go` split: provider implementations moved into per-provider files; default Anthropic model bumped to `claude-opus-4-7`, default OpenAI model to `gpt-4o-mini`.
- `cmd/nous/main.go` — selects the extractor based on `NOUS_LLM_PROVIDER` (default `ScriptedExtractor` for offline / CI runs).

### Restored
- `internal/llm/scripted.go` and its test were re-added to keep the deterministic extractor available for tests and offline demos until LLM providers are GA.

## [0.1.0] — 2026-04-30

Initial MVP. All 23 Roady tasks complete.

### Core
- Domain model: `Commitment`, `Decision`, `Intervention`, `Goal`, `Task`.
- Risk engine with configurable thresholds.
- Intervention engine (nudge / escalate / suggest / automate).
- Extractor pipeline (scripted MVP; LLM scaffolding in place).
- Evaluator pipeline for batch risk assessment.

### Adapters
- Mnemos gRPC adapter with TLS + bearer-token auth (98 % test coverage).
- Chronos gRPC adapter with TLS + bearer-token auth (98 % test coverage).

### Transport
- gRPC server (primary) with full Nous API.
- HTTP gateway with rate limiting.
- Protobuf definitions in `api/nous/v1/`.

### Storage
- In-memory backend.
- SQLite backend with migrations.
- PostgreSQL backend with migrations.
- Three-backend parity test suite.

### Observability & resilience (Phase-1 hardening, late April 2026)
- Structured JSON logging with correlation IDs.
- OpenTelemetry tracing for Extract and Evaluate pipelines.
- Atomic counter metrics for commitments extracted, evaluations run, interventions created.
- Circuit breakers for Mnemos and Chronos adapters (`internal/circuit`).
- Adapter health monitoring aggregated into `/v1/health`.
- Prometheus alerting rules in `deploy/prometheus/alerting_rules.yml`.
- Grafana dashboard in `deploy/grafana/dashboards/`.
- nox v0.7.0 baseline scan committed in `findings.json`.
