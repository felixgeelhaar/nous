package llm

import (
	"strings"
	"testing"
)

func TestPromptBuilder_DefaultVersion(t *testing.T) {
	t.Parallel()
	b := NewPromptBuilder()
	if b.Version() != PromptVersion {
		t.Errorf("version = %q", b.Version())
	}
}

func TestPromptBuilder_BuildIncludesExamples(t *testing.T) {
	t.Parallel()
	b := NewPromptBuilder()
	got := b.Build("ship the report tomorrow")
	if !strings.Contains(got, "Example 1 input:") {
		t.Error("missing example marker")
	}
	if !strings.Contains(got, "send the report") {
		t.Error("default example not embedded")
	}
	if !strings.Contains(got, "Input: ship the report tomorrow") {
		t.Error("live input missing")
	}
	if !strings.HasSuffix(got, "Output (JSON only, no prose): ") {
		t.Error("wrong terminator")
	}
}

func TestPromptBuilder_OverrideExamples(t *testing.T) {
	t.Parallel()
	b := NewPromptBuilder(WithFewShotExamples(FewShotExample{
		Input:  "ping me later",
		Output: []FewShotDraft{{Description: "ping me", Confidence: 0.6}},
	}))
	got := b.Build("x")
	if !strings.Contains(got, "ping me later") {
		t.Error("override example missing")
	}
	if strings.Contains(got, "send the report") {
		t.Error("default example leaked through override")
	}
}

func TestPromptBuilder_OverrideSystemPrompt(t *testing.T) {
	t.Parallel()
	b := NewPromptBuilder(WithSystemPrompt("CUSTOM SYSTEM"))
	got := b.Build("x")
	if !strings.Contains(got, "CUSTOM SYSTEM") {
		t.Error("custom system prompt missing")
	}
}

func TestPromptBuilder_NoExamplesAllowed(t *testing.T) {
	t.Parallel()
	b := NewPromptBuilder(WithFewShotExamples())
	got := b.Build("x")
	if strings.Contains(got, "Example") {
		t.Error("example block present despite empty examples")
	}
}
