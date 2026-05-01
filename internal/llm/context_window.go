package llm

import (
	"strings"
	"unicode/utf8"
)

// EstimateTokens returns a coarse token estimate using the
// "≈ 4 chars per token" heuristic that holds across most BPE
// tokenisers (GPT-4, Claude, Gemini are all within ~10% of this on
// English text). For commitment extraction the input is short
// natural language, so a 10% error is fine — the goal is to keep us
// under provider context limits, not to bill exactly.
func EstimateTokens(s string) int {
	if s == "" {
		return 0
	}
	bytes := utf8.RuneCountInString(s)
	return (bytes + 3) / 4
}

// ContextBudget describes a provider's input budget. MaxInputTokens
// is the hard ceiling; HeadroomTokens is reserved for the model's
// completion plus a safety margin so we don't trigger the upstream
// "context overflow" error.
type ContextBudget struct {
	MaxInputTokens int
	HeadroomTokens int
}

// Available returns the number of tokens callers may emit in the
// prompt without breaching the budget.
func (b ContextBudget) Available() int {
	a := b.MaxInputTokens - b.HeadroomTokens
	if a < 0 {
		return 0
	}
	return a
}

// FitText shrinks input so that prompt overhead + input fits in
// budget. Strategy: when input alone exceeds the available budget,
// keep the head and tail of the text and replace the middle with a
// single "[…]" marker. The head/tail split is biased toward the
// tail because commitments tend to live at the end of conversational
// snippets ("...we'll ship by Friday").
//
// overheadTokens accounts for the prompt template, system prompt,
// few-shot examples, and the JSON-output preamble. Pass an
// approximate value; FitText conservatively rounds.
//
// When input fits, FitText returns it unchanged.
func FitText(input string, overheadTokens int, budget ContextBudget) string {
	available := budget.Available() - overheadTokens
	if available <= 0 {
		return ""
	}
	if EstimateTokens(input) <= available {
		return input
	}

	// Convert the available token budget back to character budget,
	// reserve room for the elision marker.
	const marker = "\n[…]\n"
	markerTokens := EstimateTokens(marker)
	keepTokens := available - markerTokens
	if keepTokens <= 0 {
		return ""
	}
	keepChars := keepTokens * 4

	headChars := keepChars / 3
	tailChars := keepChars - headChars

	runes := []rune(input)
	if len(runes) <= headChars+tailChars {
		return input
	}
	head := strings.TrimRightFunc(string(runes[:headChars]), splitRune)
	tail := strings.TrimLeftFunc(string(runes[len(runes)-tailChars:]), splitRune)
	return head + marker + tail
}

// splitRune is the predicate FitText uses to round head/tail edges
// to nearby whitespace so the elision marker doesn't land mid-word.
func splitRune(r rune) bool {
	return r != ' ' && r != '\n' && r != '\t' && r != '.' && r != ','
}

// DefaultBudgets is the per-provider context-window budget table.
// Numbers reflect each provider's documented input ceiling minus a
// 4 KiB safety headroom for completion + tool-use overhead.
var DefaultBudgets = map[string]ContextBudget{
	"anthropic": {MaxInputTokens: 200_000, HeadroomTokens: 4_000},
	"openai":    {MaxInputTokens: 128_000, HeadroomTokens: 4_000},
	"gemini":    {MaxInputTokens: 1_000_000, HeadroomTokens: 4_000},
	"bedrock":   {MaxInputTokens: 200_000, HeadroomTokens: 4_000},
}

// BudgetFor returns the budget for provider, falling back to a
// conservative 8 K window for unknown providers.
func BudgetFor(provider string) ContextBudget {
	if b, ok := DefaultBudgets[provider]; ok {
		return b
	}
	return ContextBudget{MaxInputTokens: 8_000, HeadroomTokens: 1_000}
}
