package llm

import (
	"context"
	"strings"
	"testing"

	"github.com/felixgeelhaar/nous/internal/domain"
)

// fakeProvider records the last prompt it received.
type fakeProvider struct {
	lastPrompt string
}

func (p *fakeProvider) ExtractCommitments(_ context.Context, prompt string) ([]domain.CommitmentDraft, error) {
	p.lastPrompt = prompt
	return []domain.CommitmentDraft{{Description: "x", Confidence: 0.5}}, nil
}

func TestABRouter_DeterministicByOwner(t *testing.T) {
	t.Parallel()
	p := &fakeProvider{}
	a := NewPromptBuilder(WithSystemPrompt("VARIANT_A"))
	b := NewPromptBuilder(WithSystemPrompt("VARIANT_B"))
	r, err := NewABRouter(p, []PromptVariant{
		{Label: "a", Weight: 1, Builder: a},
		{Label: "b", Weight: 1, Builder: b},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	first, _ := r.Extract(context.Background(), ExtractInput{OwnerID: "alice", Text: "x"})
	firstVar := r.LastVariant()
	if len(first) != 1 {
		t.Fatalf("len = %d", len(first))
	}
	// Same owner → same variant on a re-run.
	r.Extract(context.Background(), ExtractInput{OwnerID: "alice", Text: "x"})
	if r.LastVariant() != firstVar {
		t.Errorf("variant flipped between calls: %q -> %q", firstVar, r.LastVariant())
	}
}

func TestABRouter_DifferentOwnersGetDifferentVariants(t *testing.T) {
	t.Parallel()
	p := &fakeProvider{}
	a := NewPromptBuilder(WithSystemPrompt("VARIANT_A"))
	b := NewPromptBuilder(WithSystemPrompt("VARIANT_B"))
	r, _ := NewABRouter(p, []PromptVariant{
		{Label: "a", Weight: 1, Builder: a},
		{Label: "b", Weight: 1, Builder: b},
	})
	// Search for two owner IDs that hash into different buckets.
	hits := map[string]bool{}
	for _, owner := range []string{"alice", "bob", "carol", "dave", "eve"} {
		_, _ = r.Extract(context.Background(), ExtractInput{OwnerID: owner, Text: "x"})
		hits[r.LastVariant()] = true
		if len(hits) == 2 {
			return
		}
	}
	t.Errorf("never saw both variants; hits=%v", hits)
}

func TestABRouter_RespectsWeights(t *testing.T) {
	t.Parallel()
	p := &fakeProvider{}
	a := NewPromptBuilder()
	b := NewPromptBuilder()
	r, _ := NewABRouter(p, []PromptVariant{
		{Label: "a", Weight: 9, Builder: a},
		{Label: "b", Weight: 1, Builder: b},
	})
	counts := map[string]int{}
	for i := 0; i < 1000; i++ {
		_, _ = r.Extract(context.Background(), ExtractInput{OwnerID: makeOwner(i), Text: "x"})
		counts[r.LastVariant()]++
	}
	if counts["a"] < counts["b"]*4 {
		t.Errorf("expected a >> b; got a=%d b=%d", counts["a"], counts["b"])
	}
}

func TestABRouter_RejectsEmptyVariants(t *testing.T) {
	t.Parallel()
	if _, err := NewABRouter(&fakeProvider{}, nil); err == nil {
		t.Fatal("want error")
	}
	if _, err := NewABRouter(&fakeProvider{}, []PromptVariant{{Label: "x"}}); err == nil {
		t.Fatal("want error on zero-weight set")
	}
}

func TestABRouter_HotSwapVariants(t *testing.T) {
	t.Parallel()
	p := &fakeProvider{}
	r, _ := NewABRouter(p, []PromptVariant{{Label: "a", Weight: 1, Builder: NewPromptBuilder()}})
	if err := r.SetVariants([]PromptVariant{{Label: "b", Weight: 1, Builder: NewPromptBuilder()}}); err != nil {
		t.Fatalf("setvariants: %v", err)
	}
	_, _ = r.Extract(context.Background(), ExtractInput{OwnerID: "x", Text: "y"})
	if r.LastVariant() != "b" {
		t.Errorf("variant = %q, want b", r.LastVariant())
	}
}

func TestABRouter_AppliesContextBudget(t *testing.T) {
	t.Parallel()
	p := &fakeProvider{}
	// Use a minimal builder so the system prompt doesn't eat the budget.
	bare := NewPromptBuilder(WithSystemPrompt("EXTRACT JSON"), WithFewShotExamples())
	r, _ := NewABRouter(p, []PromptVariant{{Label: "a", Weight: 1, Builder: bare}},
		WithContextBudget(ContextBudget{MaxInputTokens: 1000, HeadroomTokens: 50}),
	)
	long := strings.Repeat("xxxx ", 1500) // ~7500 chars, ~1875 tokens
	_, _ = r.Extract(context.Background(), ExtractInput{OwnerID: "u", Text: long})
	if !strings.Contains(p.lastPrompt, "[…]") {
		t.Error("expected elision marker in oversized prompt")
	}
}

func makeOwner(i int) string {
	return "user-" + string(rune('a'+i%26)) + string(rune('0'+i/26%10))
}
