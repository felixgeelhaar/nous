package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
)

// Provider is the LLM transport seam. Concrete adapters (Anthropic,
// OpenAI) implement this and are wired at startup.
type Provider interface {
	ExtractCommitments(ctx context.Context, prompt string) ([]domain.CommitmentDraft, error)
}

// PromptVersion identifies a prompt template version. Bumping the
// version invalidates upstream prompt caches and signals to evals
// that the harness needs a new golden snapshot.
const PromptVersion = "v2"

// PromptBuilder builds prompts for commitment extraction. Prompts
// are versioned (see [PromptVersion]); few-shot examples can be
// supplied via [WithFewShotExamples] for in-context learning.
type PromptBuilder struct {
	version      string
	systemPrompt string
	examples     []FewShotExample
}

// FewShotExample is a training example for the LLM. Output is the
// shape the model should return for Input.
type FewShotExample struct {
	Input  string
	Output []FewShotDraft
}

// FewShotDraft is the public-facing draft shape exposed to operators
// who construct in-context examples. It mirrors the JSON the model
// is asked to emit.
type FewShotDraft struct {
	Description string  `json:"description"`
	DueAt       *string `json:"due_at,omitempty"`
	Confidence  float64 `json:"confidence"`
}

// PromptOption tunes a [PromptBuilder] after construction.
type PromptOption func(*PromptBuilder)

// WithFewShotExamples replaces the builder's example set. Order is
// preserved; place the most representative example last because
// recency biases attention.
func WithFewShotExamples(examples ...FewShotExample) PromptOption {
	return func(b *PromptBuilder) { b.examples = examples }
}

// WithSystemPrompt overrides the default system instruction. Bumping
// the prompt without bumping [PromptVersion] is allowed when the
// change is purely cosmetic; semantic changes should also bump
// [PromptVersion].
func WithSystemPrompt(s string) PromptOption {
	return func(b *PromptBuilder) { b.systemPrompt = s }
}

// NewPromptBuilder creates a prompt builder with the default v2
// system prompt and a small canonical example set.
func NewPromptBuilder(opts ...PromptOption) *PromptBuilder {
	b := &PromptBuilder{
		version:      PromptVersion,
		systemPrompt: defaultSystemPrompt(),
		examples:     defaultFewShotExamples(),
	}
	for _, o := range opts {
		o(b)
	}
	return b
}

// Version returns the prompt version this builder will produce.
func (b *PromptBuilder) Version() string { return b.version }

// Build creates the full prompt for extraction. Order: system prompt,
// few-shot examples (if any), the live input.
func (b *PromptBuilder) Build(input string) string {
	var out strings.Builder
	out.WriteString(b.systemPrompt)
	out.WriteString("\n\n")
	for i, ex := range b.examples {
		fmt.Fprintf(&out, "Example %d input: %s\n", i+1, ex.Input)
		raw, err := json.Marshal(ex.Output)
		if err != nil {
			continue
		}
		fmt.Fprintf(&out, "Example %d output: %s\n\n", i+1, raw)
	}
	out.WriteString("Input: ")
	out.WriteString(input)
	out.WriteString("\nOutput (JSON only, no prose): ")
	return out.String()
}

func defaultSystemPrompt() string {
	return `You are an expert at extracting commitments from text.
Extract commitments, promises, and action items.
Return a JSON array of objects with these fields:
  - description (string, required)
  - due_at (RFC 3339 timestamp or null)
  - confidence (number in [0.0, 1.0])
  - entities (array of {kind, name} objects, optional)
Return only the JSON array. No prose, no code fences. If there are no commitments, return [].
If unsure about a commitment, set confidence < 0.5.`
}

// defaultFewShotExamples ships a small canonical set so the v2
// prompt has reasonable in-context grounding without operator
// configuration.
func defaultFewShotExamples() []FewShotExample {
	due := "2026-05-02T15:00:00Z"
	return []FewShotExample{
		{
			Input: "I'll send the report by Friday.",
			Output: []FewShotDraft{
				{Description: "send the report", DueAt: &due, Confidence: 0.9},
			},
		},
		{
			Input: "Just thinking out loud, maybe we'd consider rolling that out next quarter.",
			Output: []FewShotDraft{},
		},
	}
}

// LLMExtractor uses an LLM provider to extract commitments.
type LLMExtractor struct {
	provider Provider
	builder  *PromptBuilder
	budget   ContextBudget
}

// LLMExtractorOption tunes a LLMExtractor.
type LLMExtractorOption func(*LLMExtractor)

// WithContextBudget caps the input portion of the prompt against a
// provider's documented context window. Inputs that would exceed
// the available token budget are head-tail elided via [FitText].
func WithContextBudget(b ContextBudget) LLMExtractorOption {
	return func(e *LLMExtractor) { e.budget = b }
}

// WithPromptBuilder swaps the default builder for a custom one.
// Pair with [WithFewShotExamples] / [WithSystemPrompt] for prompt
// experimentation.
func WithPromptBuilder(b *PromptBuilder) LLMExtractorOption {
	return func(e *LLMExtractor) { e.builder = b }
}

// NewLLMExtractor creates an extractor backed by an LLM.
func NewLLMExtractor(provider Provider, opts ...LLMExtractorOption) *LLMExtractor {
	e := &LLMExtractor{
		provider: provider,
		builder:  NewPromptBuilder(),
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

// Extract sends the input to the LLM and parses the response. When
// a context budget is configured, oversized inputs are head-tail
// elided so they fit alongside prompt overhead.
func (e *LLMExtractor) Extract(ctx context.Context, in ExtractInput) ([]domain.CommitmentDraft, error) {
	text := in.Text
	if e.budget.MaxInputTokens > 0 {
		overhead := EstimateTokens(e.builder.Build(""))
		text = FitText(text, overhead, e.budget)
	}
	prompt := e.builder.Build(text)

	drafts, err := e.provider.ExtractCommitments(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("llm extract: %w", err)
	}

	for i := range drafts {
		if drafts[i].OwnerID == "" {
			drafts[i].OwnerID = in.OwnerID
		}
		drafts[i].SourceRefs = append(append([]domain.SourceRef(nil), drafts[i].SourceRefs...), in.SourceRefs...)
	}

	return drafts, nil
}

// defaultHTTPClient is the shared HTTP client for provider calls. The
// 30 s timeout is conservative for cloud LLMs; callers can override
// via the With...Client constructors.
func defaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}
