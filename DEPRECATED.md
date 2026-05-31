# DEPRECATED

**Status:** Archived 2026-05-31
**Final release:** `v0.3.1-final`
**Authoritative successor:** [decisionkit](https://github.com/felixgeelhaar/decisionkit)
for risk + intervention scoring. LLM extraction lives in agent runtimes;
storage lives in [Mnemos](https://github.com/felixgeelhaar/mnemos).

## Why this was archived

1. **Reasoning is now an agent-layer concern.** Modern agent runtimes
   (Claude Code, Codex, Hermes, Nomi, OpenClaw, NanoClaw, ...) plus
   frontier models plus tool use plus MCP cover what Nous was built to
   provide as a dedicated service. A separate Go runtime for LLM-based
   extraction is no longer the right shape.
2. **Only Olymp depended on Nous**, and Olymp is also being archived as
   part of the same cognitive-stack simplification.
3. **The deterministic value was small and reusable.** Risk scoring and
   intervention policy are pure functions with 90%+ test coverage —
   extracted verbatim into the standalone
   [decisionkit](https://github.com/felixgeelhaar/decisionkit) module so
   consumers can keep using them without the service overhead.

See [ADR 0005 in Mnemos](https://github.com/felixgeelhaar/mnemos/blob/main/docs/adr/0005-archive-nous.md)
for the full decision record.

## What survived

| Primitive | New home |
|---|---|
| Risk scorer (weighted, explainable) | [`decisionkit/risk`](https://github.com/felixgeelhaar/decisionkit/tree/main/risk) |
| Intervention policy engine | [`decisionkit/intervention`](https://github.com/felixgeelhaar/decisionkit/tree/main/intervention) |
| Pure domain types (Commitment, Intervention) | [`decisionkit/domain`](https://github.com/felixgeelhaar/decisionkit/tree/main/domain) |
| Adapter helpers for Mnemos | [`decisionkit/mnemos`](https://github.com/felixgeelhaar/decisionkit/tree/main/mnemos) |

## What did not survive

These components exist at the `v0.3.1-final` tag but were not lifted into
any new library because their value is tied to running Nous as a service:

- LLM extraction pipeline (`internal/llm`, `internal/pipeline`) with
  Anthropic / OpenAI / Gemini / Bedrock / Ollama providers
- gRPC + HTTP transport (`internal/transport`)
- Multi-backend storage (`internal/store`: memory, sqlite, postgres)
- Adapter orchestration with circuit breakers (`internal/adapters`)
- Commitment / decision / goal / task / plan domain models beyond what
  the risk + intervention engines needed

If you want LLM-based commitment extraction in 2026+, implement it as a
tool definition in your agent runtime. Mnemos can store the structured
output as a claim.

## Recovery path

```bash
git clone https://github.com/felixgeelhaar/nous.git
cd nous
git checkout v0.3.1-final
```

The repo remains read-only and publicly readable. Source, tags, and
history are preserved.

## Replacement guidance

| If you wanted... | Use instead |
|---|---|
| Deterministic risk + intervention scoring | [decisionkit](https://github.com/felixgeelhaar/decisionkit) |
| LLM-based commitment extraction | Agent runtime + tool definition |
| Persistent commitment storage | [Mnemos](https://github.com/felixgeelhaar/mnemos) |
| Multi-step reasoning loops | Agent runtime native loop + MCP into Mnemos |

## Related archives

- [Olymp](https://github.com/felixgeelhaar/olymp) — orchestration runtime, archived
- [Praxis](https://github.com/felixgeelhaar/praxis) — action service, archived
