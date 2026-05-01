# Security

## Reporting a vulnerability

Email **felix.geelhaar@gmail.com** with `[NOUS SECURITY]` in the subject. Do not open a public issue. Expect an initial response within five business days.

## Threat model

Nous extracts commitments from input text, persists them with risk scores, and emits interventions. The HTTP/gRPC server does not yet gate inbound auth — that is on the production-hardening backlog (`ROADMAP.md`). Production deployments must:

- Run behind TLS + an authenticating reverse proxy or service mesh until inbound auth lands.
- Pin Docker images to digests (already done in `Dockerfile`).
- Configure `NOUS_MNEMOS_BEARER_TOKEN` and `NOUS_CHRONOS_BEARER_TOKEN` for adapter-side calls; pair with TLS cert files.
- Treat the service as trusted-network only otherwise.

## Findings baseline (`findings.json`)

`findings.json` is the committed baseline of [`nox`](https://github.com/felixgeelhaar/nox) v0.7.0 scan results. Every finding has `Status: "baselined"` — meaning it was reviewed at MVP time and accepted as known-and-acceptable. New scans must be diffed against this file in CI; any finding **not** present in the baseline fails the build.

### Categories in the baseline

- **`CONT-001` — base-image digest pinning (medium).** Originally two rows for `golang:1.26-alpine` and `gcr.io/distroless/static-debian12:nonroot`. Now resolved (Dockerfile pins both to digests). The baseline rows can be removed on the next baseline refresh.
- **`DATA-001` — test data masquerading as PII (low confidence).** ~575 occurrences in test files: hard-coded `@example.com` emails, fake user IDs, deterministic UUIDs. Reviewed and accepted: these are test fixtures, not real data. Real-data findings would surface as new entries.
- Misc low-confidence noise that does not warrant a per-finding write-up. Refer to `findings.json` itself for the canonical list.

### Refreshing the baseline

```bash
make nox-scan        # runs nox scan against the working tree
# diff findings.json — every change must be justified in the PR description
git add findings.json
```

Adding a new file or feature that legitimately introduces a finding is allowed; the PR must describe **why** the finding is acceptable. Unexplained additions block merge.

## Container

The Docker image:

- Builds with `CGO_ENABLED=0` for a static binary.
- Pins `golang:1.26-alpine` and `gcr.io/distroless/static-debian12:nonroot` to specific digests.
- Runs as `nonroot` (UID 65532).
- Exposes only ports `50051` (gRPC) and `8080` (HTTP).

## Dependencies

Go module dependencies are tracked in `go.mod` / `go.sum`. Update via:

```bash
go get -u ./...
go mod tidy
make test       # ensure nothing broke
make nox-scan   # confirm no new dependency CVEs
```

## Secrets

No secrets are stored in source. All bearer tokens, TLS certs, and database connection strings come from environment variables at startup. See `README.md` for the full env var table.
