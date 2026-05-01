# ADR 005: Scripted LLM Extractor for MVP

## Status
Accepted (2026-04). Active: `ScriptedExtractor` remains the deterministic fallback for tests and offline demos. Phase-3 LLM provider transition in progress — see "Future Work" below and `internal/llm/llm_extractor.go`.

## Context
Commitment extraction is the core value proposition of Nous — turning natural language into structured commitments. However, LLM integration introduces:
- External dependencies and latency
- Cost and rate limiting
- Non-deterministic behavior that's hard to test

## Decision
Implement a **scripted extractor** (`internal/llm/scripted.go`) for the MVP that uses deterministic regex and keyword patterns, with the `llm.CommitmentExtractor` interface designed for future LLM implementations.

The scripted extractor:
- Recognizes commitment phrases ("I will", "I promise to", "need to", etc.)
- Extracts deadlines with relative time parsing ("tomorrow", "by Friday")
- Assigns confidence scores based on phrase strength
- Returns empty results for non-commitment text

## Consequences

### Positive
- Fully deterministic and testable
- Zero external dependencies or API costs
- Fast execution (microseconds per request)
- Interface allows seamless swap to real LLM later

### Negative
- Lower accuracy than a real LLM on complex or implicit commitments
- No semantic understanding (e.g., "get that done" without context)
- Limited language support

## Alternatives Considered
- **OpenAI GPT-4**: Best accuracy but adds cost, latency, and external dependency
- **Local LLM (ollama)**: Better privacy but adds deployment complexity
- **Hybrid approach**: Use scripted for fast path, LLM for ambiguous cases — deferred to post-MVP

## Future Work
Post-MVP, implement a `llm.OpenAIExtractor` or `llm.AnthropicExtractor` that calls external APIs with:
- Structured output (JSON mode / function calling)
- Few-shot prompting with examples
- Confidence calibration

## Phase-3 Transition (in progress, May 2026)
`internal/llm/llm_extractor.go` introduces `LLMExtractor` and `Provider` seam alongside `AnthropicProvider` and `OpenAIProvider` stubs. Both providers currently return `not yet implemented`; HTTP transport and JSON parsing land next. `ScriptedExtractor` continues to satisfy the `CommitmentExtractor` port and is wired by default for tests and demos until the LLM path is GA. ADR-006 will record the cutover decision.
