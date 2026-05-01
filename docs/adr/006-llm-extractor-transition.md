# ADR 006: LLM Extractor Transition

## Status
Accepted (2026-05). Supersedes the "Future Work" section of [ADR 005](005-scripted-llm-extractor.md). `ScriptedExtractor` retained as the deterministic fallback.

## Context

ADR-005 shipped the MVP with a deterministic regex/keyword extractor. It bought us:
- zero external dependencies for tests, demos, and offline runs;
- bit-stable output for golden tests;
- microsecond-scale latency.

It did not buy us:
- semantic understanding ("get that done" with no antecedent — invisible to scripted rules);
- robustness to paraphrase ("can you remind me about X" vs "ping me about X");
- entity disambiguation;
- temporal phrase parsing beyond a small fixed vocabulary.

Phase-3 lifts the lid on those by adding cloud LLM providers behind the existing `CommitmentExtractor` port. The wiring change must keep the deterministic path available so test, demo, and air-gapped deployments aren't broken.

## Decision

### Provider seam

`internal/llm/llm_extractor.go` defines a small `Provider` port:

```go
type Provider interface {
    ExtractCommitments(ctx context.Context, prompt string) ([]domain.CommitmentDraft, error)
}
```

`LLMExtractor` is the `CommitmentExtractor` adapter. It owns the prompt builder; the provider owns the transport. This split lets a single prompt evolution roll out across providers without touching their HTTP code.

### Concrete providers (shipped)

- `internal/llm/anthropic.go` — Anthropic Messages API (`POST /v1/messages`). Headers: `x-api-key`, `anthropic-version: 2023-06-01`. Default model `claude-opus-4-7` (configurable via `NOUS_LLM_MODEL`). Tested via `httptest.Server`.
- `internal/llm/openai.go` — OpenAI Chat Completions (`POST /v1/chat/completions`) with `response_format: json_object`. Default model `gpt-4o-mini`. Wraps the prompt with a `commitments` key requirement; falls back to bare-array parse for prompts that ignored the wrapper.

Both providers accept a `BaseURL` option so tests, OpenAI-compatible endpoints (vLLM, llama.cpp), and corporate proxies can substitute the upstream.

### Config

Three new env vars in `config.LLMConfig`:

- `NOUS_LLM_PROVIDER` — `anthropic`, `openai`, or empty (use `ScriptedExtractor`).
- `NOUS_LLM_API_KEY` — required when `NOUS_LLM_PROVIDER` is set; validation fails fast at startup.
- `NOUS_LLM_MODEL` — optional override.
- `NOUS_LLM_BASE_URL` — optional override (required for OpenAI-compatible endpoints).

`buildCommitmentExtractor` in `cmd/nous/main.go` is the dispatch point. Default (no provider) returns `llm.NewScriptedExtractor()` — every existing test and offline deployment continues to work unchanged.

### Output parsing

`internal/llm/parse.go` is a shared, transport-independent parser:
- Strips code fences (` ```json ... ``` `).
- Trims one-line preambles ("Here's the JSON:").
- Locates the first balanced JSON array; returns the slice from `[` if brackets don't balance so `json.Unmarshal` surfaces the parse error.
- Clamps confidence into `[0, 1]`.
- Drops drafts with empty descriptions.
- Accepts RFC 3339, RFC 3339 with no zone, `2006-01-02 15:04:05`, or date-only `due_at` strings; failures degrade gracefully (`due_at` left nil, draft survives).

### Failure modes

- **Provider error**: returned as a wrapped `error` from `Extract`. The pipeline propagates this; the caller sees a non-zero error count and zero saved.
- **Malformed model output**: `parseDrafts` returns a parse error. Same propagation.
- **Empty content**: zero drafts, no error — equivalent to "we ran the LLM and it found nothing".

The pipeline does **not** silently fall back to `ScriptedExtractor` on LLM failure. Operators choose one path; surfacing the failure is preferable to mixed-provenance commitments in the audit trail.

## Consequences

### Positive
- Production LLM extraction is now a config flip, not a code change.
- Provider implementations are independently testable via `httptest`; CI runs without API keys.
- The deterministic fallback keeps the development feedback loop fast and offline-capable.
- Adding a new provider (Gemini, Bedrock, OpenAI-compatible) is a single new file in `internal/llm/` — no changes anywhere else.

### Negative
- Two extractors are now in the test surface; we accept that as the cost of the offline guarantee.
- The shared parser accepts more shapes than the strict spec we ask for, which can mask prompt regressions. Mitigation: provider tests assert the strict-shape happy path, and prompt iterations should add golden tests when adding a new accepted variation.

## Alternatives considered

- **Anthropic only**: Rejected. We have customers locked to OpenAI key budgets.
- **Direct in-tree replacement of `ScriptedExtractor`**: Rejected. CI without API keys is non-negotiable.
- **Offline LLM via `llama.cpp` shipped in-tree**: Rejected for v1 — large binary footprint and runtime model files don't fit the "single static binary" goal. The `BaseURL` option lets operators wire a local OpenAI-compatible server if they want this.

## Forward-looking

- Few-shot prompting and prompt versioning will land via the `PromptBuilder`. The provider seam stays unchanged.
- Streaming responses (server-sent events) are not used today — extraction is short enough that batched calls are simpler and cheaper to retry.
- A future ADR will record the prompt-evaluation harness (offline accuracy / precision / recall) once we have golden datasets.
