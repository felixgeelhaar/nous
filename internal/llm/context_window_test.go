package llm

import (
	"strings"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	t.Parallel()
	cases := map[string]int{
		"":     0,
		"abcd": 1,
		"abcdefgh": 2,
	}
	for in, want := range cases {
		if got := EstimateTokens(in); got != want {
			t.Errorf("EstimateTokens(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestFitText_Passthrough(t *testing.T) {
	t.Parallel()
	got := FitText("short input", 100, ContextBudget{MaxInputTokens: 1000, HeadroomTokens: 100})
	if got != "short input" {
		t.Errorf("got = %q", got)
	}
}

func TestFitText_TrimsLargeInput(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("aaaa ", 1000) // ~5000 chars, ~1250 tokens
	got := FitText(long, 100, ContextBudget{MaxInputTokens: 500, HeadroomTokens: 50})
	if !strings.Contains(got, "[…]") {
		t.Error("expected elision marker")
	}
	if EstimateTokens(got) > 500 {
		t.Errorf("trimmed tokens = %d, want ≤ 500", EstimateTokens(got))
	}
}

func TestFitText_RespectsBudget(t *testing.T) {
	t.Parallel()
	// Available = 100 tokens; overhead 30 → 70 left for input+marker.
	long := strings.Repeat("xxxx ", 200)
	got := FitText(long, 30, ContextBudget{MaxInputTokens: 100, HeadroomTokens: 0})
	if EstimateTokens(got) > 70 {
		t.Errorf("got %d tokens, budget 70", EstimateTokens(got))
	}
}

func TestFitText_ZeroBudget(t *testing.T) {
	t.Parallel()
	got := FitText("anything", 1000, ContextBudget{MaxInputTokens: 100, HeadroomTokens: 100})
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestBudgetFor_KnownProviders(t *testing.T) {
	t.Parallel()
	for _, p := range []string{"anthropic", "openai", "gemini", "bedrock"} {
		b := BudgetFor(p)
		if b.MaxInputTokens == 0 {
			t.Errorf("%s budget zero", p)
		}
	}
}

func TestBudgetFor_UnknownProviderFallsBack(t *testing.T) {
	t.Parallel()
	b := BudgetFor("mystery")
	if b.MaxInputTokens != 8_000 {
		t.Errorf("fallback = %d, want 8000", b.MaxInputTokens)
	}
}

func TestFitText_KeepsHeadAndTail(t *testing.T) {
	t.Parallel()
	long := "HEAD " + strings.Repeat("middle ", 500) + " TAIL"
	got := FitText(long, 0, ContextBudget{MaxInputTokens: 200, HeadroomTokens: 0})
	if !strings.Contains(got, "HEAD") {
		t.Error("head dropped")
	}
	if !strings.Contains(got, "TAIL") {
		t.Error("tail dropped")
	}
}
