# Service Level Objectives

These are the formal SLOs Nous targets in production. Alerting rules in [`deploy/prometheus/alerting_rules.yml`](../deploy/prometheus/alerting_rules.yml) check shorter-window manifestations of each SLO; this document is the canonical statement.

## Headline SLOs

| SLO | Target | Window | Source signal |
|---|---|---|---|
| Availability | 99.9 % | 30 days, rolling | `up{job="nous"}` from Prometheus blackbox |
| HTTP p99 latency | < 100 ms | 30 days, rolling | `nous_http_request_duration_seconds_bucket` |
| HTTP error rate | < 0.1 % | 30 days, rolling | `nous_http_request_duration_seconds_count{status="error"}` |
| Adapter availability | 99.5 % | 30 days, rolling | `nous_adapter_status{name="mnemos\|chronos"} == "healthy"` |

## Error budgets

A 30-day budget at 99.9 % availability allows **43 minutes** of unavailability per window. Burn rate alerts (multi-window, multi-burn-rate) fire when the budget is on track to be consumed faster than allowed:

- **Fast burn** — 14.4× budget burn over 1h. Page immediately.
- **Slow burn** — 6× budget burn over 6h. Ticket / next-business-day.
- **Trickle burn** — 3× budget burn over 3d. Track in weekly review.

Adapter availability uses a separate budget (99.5 %, 30 days, ~3.6 hours unavailable per window). Mnemos and Chronos outages should not consume the headline availability budget — the circuit breaker keeps Nous up while degrading specific endpoints.

## Latency methodology

p99 is computed over 5-minute buckets, then aggregated to the 30-day window with a percentile-of-percentiles. This is approximate; for compliance reporting use the per-bucket values rather than a derived per-window number.

Endpoints excluded from the latency SLO:

- `/v1/extract` when an LLM provider is configured (network-bound to upstream LLM).
- `/v1/evaluate` (batch operation; latency is a function of dataset size).

## Error classification

A request counts toward the error budget when it returns 5xx **or** when an internal middleware records an unhandled error in the metric pipeline. 4xx responses are explicitly **not** SLO-impacting — they are caller errors.

Health-check failures (when the aggregated `/v1/health` returns 503) count toward availability, not error rate.

## Reporting

The Grafana dashboard [`deploy/grafana/dashboards/nous-overview.json`](../deploy/grafana/dashboards/nous-overview.json) has a dedicated SLO row with current attainment and remaining budget.

Quarterly review: any SLO breached for two consecutive quarters triggers a service-level objective revision (either tighten the operational practice or relax the target with stakeholder sign-off).
