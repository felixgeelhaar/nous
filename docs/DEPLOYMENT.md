# Deployment Runbook

Operator-facing notes for running Nous in production. See `README.md` for the configuration table and `SECURITY.md` for the trust boundary.

## Topology

```
                     ┌──────────┐
clients (HTTP/gRPC) ─┤  Nous    ├──── Mnemos (gRPC, TLS + bearer token)
                     │  pod     ├──── Chronos (gRPC, TLS + bearer token)
                     └────┬─────┘
                          │
                   SQLite / Postgres
```

Both transports run from the same process. Set `NOUS_GRPC_ADDR` (default `:50051`) and `NOUS_HTTP_ADDR` (empty disables HTTP).

## Container

The image is `gcr.io/distroless/static-debian12:nonroot` based; runs as UID 65532. Build:

```bash
docker build -t ghcr.io/felixgeelhaar/nous:$(git rev-parse --short HEAD) .
```

Both base images are pinned to digests in the `Dockerfile`. Refresh digests with `docker buildx imagetools inspect <image>:<tag>` and update the `Dockerfile` in the same PR as the image bump.

## Kubernetes outline

A reference manifest is intentionally not committed (every deployment shapes its ingress/secret-management differently). Minimum requirements:

- **Workload kind**: `Deployment` (stateless except for SQLite — use Postgres for replicated deployments).
- **Replicas**: ≥ 2 for HA when using Postgres; SQLite forces single-replica.
- **Probes**: `livenessProbe` and `readinessProbe` against `GET /v1/health`. The endpoint aggregates DB connectivity plus Mnemos/Chronos adapter health, so unhealthy downstreams will mark the pod NotReady.
- **Resources**: start at `requests: 100m CPU / 128Mi`, `limits: 1 CPU / 512Mi`. Tune via the Grafana dashboard (`deploy/grafana/dashboards/nous-overview.json`).
- **Security context**: `runAsNonRoot: true`, `readOnlyRootFilesystem: true`, `allowPrivilegeEscalation: false`, drop all capabilities.
- **Secrets**: `NOUS_DB_DSN`, `NOUS_MNEMOS_BEARER_TOKEN`, `NOUS_CHRONOS_BEARER_TOKEN` — mount as env vars from a `Secret`. TLS certs (`NOUS_MNEMOS_TLS_CERT_FILE`, `NOUS_CHRONOS_TLS_CERT_FILE`) — mount from `ConfigMap` or `Secret`.
- **Graceful shutdown**: process honours SIGTERM and drains in-flight gRPC/HTTP requests for up to 10 s. Set `terminationGracePeriodSeconds: 30`.

## Observability stack

### Prometheus

Scrape `/metrics` on the HTTP port (default 8080):

```yaml
scrape_configs:
  - job_name: nous
    static_configs:
      - targets: ['nous:8080']
    metrics_path: /metrics
    scrape_interval: 15s
```

Load alerting rules from [`deploy/prometheus/alerting_rules.yml`](../deploy/prometheus/alerting_rules.yml). The rules cover:

- `NousUptimeLow` — uptime SLO (99.9 %).
- `NousLatencyHigh` — p99 HTTP latency SLO (100 ms).
- `NousErrorRateHigh` — HTTP error rate SLO (0.1 %).
- Adapter circuit-breaker open conditions for Mnemos and Chronos.
- High-risk commitment alerts (risk score > 0.8).

### Grafana

Import [`deploy/grafana/dashboards/nous-overview.json`](../deploy/grafana/dashboards/nous-overview.json). The dashboard panels:

- Request rate and p50 / p95 / p99 latency by endpoint.
- Adapter health (Mnemos, Chronos) with circuit-breaker state.
- Commitment / intervention throughput.
- Risk-score histogram.
- Background evaluator tick lag.

## Operational procedures

### First-time bring-up

1. Provision a Postgres database (or accept SQLite for single-node).
2. Run a one-off pod with `NOUS_DB_DSN` set; the schema migrates automatically on first boot.
3. Smoke-test: `curl -fsS http://<host>:8080/v1/health` should return `{"status":"healthy"}`.
4. Run an end-to-end extract: `curl -X POST http://<host>:8080/v1/extract -d '{"owner_id":"demo","text":"I will follow up tomorrow"}'`.

### Rolling out a new release

1. Bump image digest in your manifest; `kubectl rollout restart deployment/nous` (or your equivalent).
2. Watch `NousUptimeLow` and `NousErrorRateHigh` for 5 min after rollout.
3. If the Grafana adapter-health panel goes red, pause and check Mnemos / Chronos logs before continuing.

### Adapter outage

1. Check the `/v1/health` payload — the unhealthy adapter shows up by name.
2. Check the circuit breaker metric (`nous_circuit_breaker_state`); a tripped breaker means Nous is short-circuiting calls and degrading gracefully.
3. Resolve the upstream issue; the breaker auto-resets after the cooldown.
4. No restart is required.

### Database migration

Schema lives in `sql/sqlite/migrations/` and `sql/postgres/migrations/`. Migrations run on startup; running an older binary against a newer schema is fine, the reverse is not. Always roll forward.

### Rotating the database password

Update the `Secret` and roll the deployment. Connections are pooled; the next pool re-establishment picks up the new credential. No drain required.

### Capturing a debug bundle

```bash
kubectl logs -l app=nous --tail=10000 > nous.log
kubectl get events --field-selector involvedObject.name=<pod> > nous.events
curl -fsS http://<host>:8080/metrics > nous.metrics
```

Attach the three files when escalating.

## Backup and restore

For Postgres: standard `pg_dump`. For SQLite: stop the pod, copy the `.db` file out of the persistent volume.

There is no application-level backup yet. Lessons + decisions are append-only, so a restored snapshot will agree with the source at the snapshot moment; replays of stale events are safe (idempotent on UUID).

## Known limitations

- Inbound auth is not yet wired (see `SECURITY.md`); deploy behind an authenticating proxy.
- Single-replica only on SQLite.
- No multi-tenant isolation in storage.
