# Nous — Archived

> **This repository is archived as of 2026-05-31.** Final release: `v0.3.1-final`.
> The reasoning primitive has moved into the agent layer. The two parts of
> Nous worth preserving (risk + intervention scoring) now live in a small
> standalone library, **decisionkit**.

## What to use instead

| If you wanted... | Use instead |
|---|---|
| Deterministic risk + intervention scoring (the durable value of Nous) | [decisionkit](https://github.com/felixgeelhaar/decisionkit) |
| LLM-based commitment extraction | Your agent runtime (Claude Code, Codex, Hermes, Nomi, ...) with a tool definition |
| Persistent commitment / decision storage | [Mnemos](https://github.com/felixgeelhaar/mnemos) — claim it as a memory item |
| Multi-step reasoning loops | Agent runtime + tools + MCP into Mnemos |
| Operational orchestration | Was Olymp; also archived. The agent runtime owns the loop. |

## Why this was archived

1. **Reasoning has moved into the agent layer.** Frontier models plus tool
   use plus MCP subsume what Nous was built to provide as a dedicated
   service. Maintaining a separate Go runtime for LLM extraction is no
   longer the right shape.
2. **The only Go consumer was Olymp**, which is also being archived in the
   same initiative.
3. **The deterministic value (risk + intervention) was small and
   reusable** — extracted into [decisionkit](https://github.com/felixgeelhaar/decisionkit).
   The 90%+ test coverage came along.

See [ADR 0005 in Mnemos](https://github.com/felixgeelhaar/mnemos/blob/main/docs/adr/0005-archive-nous.md)
for the full decision record.

## What was preserved

The full Nous source code is preserved at the `v0.3.1-final` tag. The
risk and intervention engines were extracted verbatim into
[decisionkit](https://github.com/felixgeelhaar/decisionkit) (ADR 0004) so
consumers can keep using the deterministic scoring without resurrecting
the service.

Other Nous components (gRPC + HTTP transports, multi-backend storage,
LLM extractor with Anthropic / OpenAI / Gemini / Bedrock / Ollama
providers, adapter orchestration with circuit breakers, commitment +
decision repositories) were **not** extracted. They exist to support a
service deployment that no longer has consumers.

## Recovery path

```bash
git clone https://github.com/felixgeelhaar/nous.git
cd nous
git checkout v0.3.1-final
```

The repo remains read-only and publicly readable indefinitely.

## Related archives

- [Olymp](https://github.com/felixgeelhaar/olymp) — orchestration runtime, archived
- [Praxis](https://github.com/felixgeelhaar/praxis) — action service, archived

## License

Unchanged from the v0.3.1-final tag. See `LICENSE`.
