package llm

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/felixgeelhaar/nous/internal/domain"
)

// stubProvider returns canned drafts or an error.
type stubProvider struct {
	drafts []domain.CommitmentDraft
	err    error
	last   string
}

func (s *stubProvider) ExtractCommitments(_ context.Context, prompt string) ([]domain.CommitmentDraft, error) {
	s.last = prompt
	if s.err != nil {
		return nil, s.err
	}
	return s.drafts, nil
}

func TestLLMExtractor_DefaultBuilder(t *testing.T) {
	t.Parallel()
	p := &stubProvider{drafts: []domain.CommitmentDraft{{Description: "x", Confidence: 0.8}}}
	e := NewLLMExtractor(p)
	got, err := e.Extract(context.Background(), ExtractInput{OwnerID: "u1", Text: "hello"})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].OwnerID != "u1" {
		t.Errorf("owner stamping failed: %q", got[0].OwnerID)
	}
}

func TestLLMExtractor_WithCustomBuilder(t *testing.T) {
	t.Parallel()
	p := &stubProvider{}
	pb := NewPromptBuilder(WithSystemPrompt("MARKER"), WithFewShotExamples())
	e := NewLLMExtractor(p, WithPromptBuilder(pb))
	_, _ = e.Extract(context.Background(), ExtractInput{Text: "x"})
	if !strings.Contains(p.last, "MARKER") {
		t.Errorf("custom system prompt not used: %q", p.last)
	}
}

func TestLLMExtractor_ContextBudgetTrims(t *testing.T) {
	t.Parallel()
	p := &stubProvider{}
	e := NewLLMExtractor(p,
		WithPromptBuilder(NewPromptBuilder(WithSystemPrompt("S"), WithFewShotExamples())),
		WithContextBudget(ContextBudget{MaxInputTokens: 200, HeadroomTokens: 20}),
	)
	long := strings.Repeat("xxxx ", 1000)
	_, _ = e.Extract(context.Background(), ExtractInput{Text: long})
	if !strings.Contains(p.last, "[…]") {
		t.Errorf("expected elision marker: prompt=%q", p.last[:min(len(p.last), 200)])
	}
}

func TestLLMExtractor_ProviderErrorPropagates(t *testing.T) {
	t.Parallel()
	p := &stubProvider{err: errors.New("upstream down")}
	e := NewLLMExtractor(p)
	_, err := e.Extract(context.Background(), ExtractInput{Text: "x"})
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "upstream down") {
		t.Errorf("err = %v", err)
	}
}

func TestLLMExtractor_PreservesSourceRefs(t *testing.T) {
	t.Parallel()
	p := &stubProvider{drafts: []domain.CommitmentDraft{
		{Description: "x", Confidence: 0.8, SourceRefs: []domain.SourceRef{{Kind: "preexisting", Locator: "k0"}}},
	}}
	e := NewLLMExtractor(p)
	in := ExtractInput{
		Text:       "x",
		SourceRefs: []domain.SourceRef{{Kind: "in", Locator: "k1"}},
	}
	got, err := e.Extract(context.Background(), in)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(got[0].SourceRefs) != 2 {
		t.Errorf("expected merged source refs (2), got %d", len(got[0].SourceRefs))
	}
}

func TestEstimateTokens_Boundaries(t *testing.T) {
	t.Parallel()
	if EstimateTokens("") != 0 {
		t.Error("empty != 0")
	}
	if EstimateTokens("a") != 1 {
		t.Error("1-char != 1 token (round up)")
	}
}

func TestContextBudget_AvailableClampsToZero(t *testing.T) {
	t.Parallel()
	b := ContextBudget{MaxInputTokens: 50, HeadroomTokens: 100}
	if b.Available() != 0 {
		t.Errorf("over-headroom: %d, want 0", b.Available())
	}
}

func TestReport_PassRateOnEmpty(t *testing.T) {
	t.Parallel()
	r := Report{}
	if r.PassRate() != 0 {
		t.Errorf("empty PassRate = %v", r.PassRate())
	}
}

func TestLoadGoldenDataset_FileMissing(t *testing.T) {
	t.Parallel()
	_, err := LoadGoldenDataset("/nonexistent/golden.json")
	if err == nil {
		t.Fatal("want error")
	}
}

func TestLoadGoldenDataset_Malformed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := dir + "/bad.json"
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadGoldenDataset(path)
	if err == nil {
		t.Fatal("want error")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
