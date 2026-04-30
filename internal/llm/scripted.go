package llm

import (
	"context"
	"strings"

	"github.com/felixgeelhaar/nous/internal/domain"
)

// ScriptedExtractor is the deterministic implementation we use in
// tests, demos, and any environment where calling an LLM is
// undesirable. Construction is intentionally minimal: callers pass
// a list of recognisers; the first one whose Trigger predicate
// matches an input emits the configured drafts.
//
// The point isn't to be smart — it's to give the pipeline
// reproducible outputs so the higher-level workflows can be tested
// without network or model variance.
type ScriptedExtractor struct {
	rules []ScriptRule
}

// ScriptRule is one entry in the script. Trigger is evaluated
// case-insensitively against the input text; Drafts is the canned
// output. SourceRefs from the input flow into each draft so the
// pipeline retains provenance.
type ScriptRule struct {
	Trigger func(text string) bool
	Drafts  []domain.CommitmentDraft
}

// NewScriptedExtractor returns an extractor configured with the
// given rules. Order is significant: the first matching rule wins.
func NewScriptedExtractor(rules ...ScriptRule) *ScriptedExtractor {
	return &ScriptedExtractor{rules: rules}
}

// ContainsAny is a small helper for ScriptRule.Trigger so callers
// don't have to spell out a closure for the common case. Match is
// case-insensitive across all needles.
func ContainsAny(needles ...string) func(string) bool {
	lowered := make([]string, len(needles))
	for i, n := range needles {
		lowered[i] = strings.ToLower(n)
	}
	return func(text string) bool {
		t := strings.ToLower(text)
		for _, n := range lowered {
			if strings.Contains(t, n) {
				return true
			}
		}
		return false
	}
}

// Extract evaluates the rules in order and returns the first
// match's drafts (annotated with the input's owner and source refs).
// No match means no drafts — and no error: extraction failure is
// "I didn't find anything", not a programmer mistake.
func (e *ScriptedExtractor) Extract(_ context.Context, in ExtractInput) ([]domain.CommitmentDraft, error) {
	for _, rule := range e.rules {
		if rule.Trigger == nil || !rule.Trigger(in.Text) {
			continue
		}
		out := make([]domain.CommitmentDraft, len(rule.Drafts))
		for i, d := range rule.Drafts {
			d.OwnerID = orDefault(d.OwnerID, in.OwnerID)
			// Append rather than overwrite so caller-supplied refs
			// inside the rule (e.g. a fixed test fixture) aren't
			// lost when the input also carries provenance.
			d.SourceRefs = append(append([]domain.SourceRef(nil), d.SourceRefs...), in.SourceRefs...)
			out[i] = d
		}
		return out, nil
	}
	return nil, nil
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
