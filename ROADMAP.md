# Nous - Future Work & Roadmap

## Current Status (April 30, 2026)

### ✅ Completed MVP (All 23 Roady Tasks)

**Core Engine:**
- Domain model (`Commitment`, `Decision`, `Intervention`, `Goal`, `Task`)
- Ports/interfaces for all storage backends
- Three storage backends: in-memory, SQLite, PostgreSQL (parity suite passing)
- Risk engine with configurable thresholds
- Intervention engine with nudge/escalate/suggest/automate actions
- Extractor pipeline with LLM integration (scripted + prompt-based)
- Evaluator pipeline for batch risk assessment

**Adapters:**
- **Mnemos gRPC Adapter** (`internal/adapters/mnemos/`) - connects to Mnemos event store
  - TLS cert support (`tlsCertFile`)
  - Bearer token authentication (`bearerToken`)
  - 98% test coverage with mock gRPC server tests
- **Chronos gRPC Adapter** (`internal/adapters/chronos/`) - connects to Chronos scheduling service
  - TLS cert support
  - Bearer token authentication
  - 98% test coverage

**Transport & APIs:**
- gRPC server (`internal/transport/grpc/`) - full Nous API implementation
- HTTP gateway (`internal/transport/http/`) - REST endpoints with rate limiting
- Protobuf definitions in `api/nous/v1/`

**Infrastructure:**
- Configuration system (`internal/config/`) with env var support
- Worker system (`internal/worker/`) for background processing
- Observability (`internal/observability/`) - structured logging, metrics, tracing
- Coordination layer (`internal/coordination/`) for distributed consensus

**Quality:**
- All packages >90% test coverage (except adapters at 98%)
- Linter clean (golangci-lint with errcheck, govet, staticcheck, unused)
- e2e tests for both gRPC and HTTP transports
- Three-backend parity test suite
- Database migrations for SQLite and PostgreSQL

---

## Next Steps (Priority Order)

### 1. Production Hardening (Next 2 Weeks)

**Security & Compliance:**
- [ ] Run full security audit (nox scan)
- [ ] Implement input validation with CEL (Custom Expr Lang)
- [ ] Add mTLS between services (AxiKernel integration)
- [ ] Rotate bearer tokens automatically
- [ ] Add rate limiting per-owner instead of global

**Monitoring & Alerting:**
- [ ] Deploy Prometheus + Grafana dashboard
- [ ] Set up alerting on high-risk commitments (> 0.8 risk score)
- [ ] Add SLOs: 99.9% uptime, p99 latency < 100ms
- [ ] Structured log shipping to ELK/Loki stack

**Resilience:**
- [ ] Circuit breakers for Mnemos/Chronos adapters
- [ ] Retry with exponential backoff for external calls
- [ ] Graceful degradation when adapters unavailable
- [ ] Health check endpoints for k8s liveness/readiness

### 2. Praxis Integration (Month 2)

**Prerequisite:** Praxis service must exist

- [ ] Build Praxis gRPC adapter (`internal/adapters/praxis/`)
- [ ] Implement `ports.PraxisClient` interface
- [ ] Wire into extractor pipeline
- [ ] Add to main entrypoint with TLS + auth
- [ ] e2e tests with mock Praxis server

### 3. AI/LLM Improvements (Month 2-3)

**Prompt Engineering:**
- [ ] Replace scripted extractor with real LLM (Claude/GPT)
- [ ] Implement context window management
- [ ] Add few-shot examples for better extraction
- [ ] A/B test prompt versions

**Model Evaluation:**
- [ ] Track extraction accuracy (precision/recall)
- [ ] Build feedback loop from intervention outcomes
- [ ] Dynamic confidence thresholding
- [ ] Multi-model ensemble for high-stakes commitments

### 4. Scale & Performance (Month 3-4)

**Database Optimization:**
- [ ] Add missing indexes based on query patterns
- [ ] Partition commitments table by date (PostgreSQL)
- [ ] Connection pooling tuning for high concurrency
- [ ] Read replicas for GET endpoints

**Caching:**
- [ ] Redis cache for hot commitments (risk score < 0.3)
- [ ] Cache prompt templates and few-shot examples
- [ ] CDN for static HTTP responses

**Async Processing:**
- [ ] Move evaluation to async job queue (Temporal/Asynq)
- [ ] Batch LLM calls for cost optimization
- [ ] Webhook callbacks for long-running operations

### 5. User-Facing Features (Month 4-6)

**Dashboard:**
- [ ] Build React/Vue dashboard for commitment tracking
- [ ] Real-time risk score updates via WebSocket
- [ ] Intervention management UI (accept/reject/escalate)
- [ ] Commitment analytics (completion rates, risk trends)

**Integrations:**
- [ ] Slack/Teams bot for commitment extraction from messages
- [ ] Email ingestion (IMAP/Exchange) for commitment parsing
- [ ] Calendar integration (Google/Outlook) for due date sync
- [ ] CRM integrations (Salesforce/HubSpot) for deal commitments

### 6. Advanced Features (Month 6+)

**Multi-Tenancy:**
- [ ] Tenant isolation in database (row-level security)
- [ ] Per-tenant LLM API keys and rate limits
- [ ] Tenant-specific extraction rules and prompt templates

**Advanced Risk Modeling:**
- [ ] ML-based risk scoring (XGBoost/LightGBM)
- [ ] Incorporate historical user behavior patterns
- [ ] External signals (calendar busy-ness, email volume)
- [ ] Network effects (if person A misses, increase risk for person B)

**Collaborative Commitments:**
- [ ] Multi-party commitments with joint accountability
- [ ] Commitment dependencies (must complete X before Y)
- [ ] Sub-commitments and decomposition
- [ ] Progress tracking and partial completion

---

## Technical Debt & Improvements

### Code Quality
- [ ] Replace `errcheck` suppression with proper error handling in tests
- [ ] Add integration test suite (real Mnemos + Chronos)
- [ ] Fuzz testing for extraction pipeline
- [ ] Property-based testing for domain invariants

### Developer Experience
- [ ] Add `make dev` target with hot-reload
- [ ] Docker Compose with all dependencies
- [ ] Generate API documentation from protobuf
- [ ] Add pre-commit hooks for formatting + linting

### Documentation
- [ ] Architecture Decision Records (ADRs) for key choices
- [ ] API reference documentation (Swagger/OpenAPI)
- [ ] Deployment runbooks (k8s, systemd, Docker)
- [ ] Video walkthroughs for new developers

---

## Release Timeline

| Version | Target Date | Key Features |
|---------|-------------|---------------|
| v0.1.0  | Week 1 (Done) | MVP with all 23 tasks complete |
| v0.2.0  | Week 4 | Production hardening, security audit |
| v0.3.0  | Week 8 | Praxis integration (if available) |
| v0.4.0  | Week 12 | AI/LLM improvements |
| v1.0.0  | Week 16 | Dashboard + initial integrations |
| v2.0.0  | Week 24 | Advanced features + multi-tenancy |

---

## Key Metrics to Track

**Reliability:**
- Uptime SLA: 99.9%
- p50/p95/p99 latency for Extract/Evaluate calls
- Error rate by endpoint and adapter

**Business Value:**
- Commitments extracted per day
- Intervention acceptance rate (target: > 70%)
- Risk prediction accuracy (precision/recall)
- Time saved vs manual tracking (survey users)

**Cost:**
- LLM API costs per commitment extracted
- Infrastructure costs (compute, database, networking)
- Support tickets per active user

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines.

---

**Last Updated:** April 30, 2026  
**Next Review:** May 15, 2026
