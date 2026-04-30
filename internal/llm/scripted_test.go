package llm_test

import (
	"context"
	"testing"
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/llm"
)

func TestScriptedExtractor_FirstMatchingRuleWins(t *testing.T) {
	t.Parallel()
	due := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)

	ext := llm.NewScriptedExtractor(
		llm.ScriptRule{
			Trigger: llm.ContainsAny("follow up", "ping"),
			Drafts: []domain.CommitmentDraft{{
				Description: "follow up with alex",
				DueAt:       &due,
				Confidence:  0.9,
				Entities:    []domain.EntityRef{{Kind: domain.EntityPerson, Name: "Alex"}},
			}},
		},
		llm.ScriptRule{
			Trigger: llm.ContainsAny("ship"),
			Drafts: []domain.CommitmentDraft{{
				Description: "ship report",
				Confidence:  0.7,
			}},
		},
	)

	cases := []struct {
		name  string
		text  string
		owner string
		want  int
	}{
		{"matches first rule", "I'll follow up with Alex tomorrow", "u1", 1},
		{"matches second rule", "Will ship the report Friday", "u1", 1},
		{"no match", "this is not a commitment", "u1", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			drafts, err := ext.Extract(context.Background(), llm.ExtractInput{
				OwnerID: tc.owner,
				Text:    tc.text,
				SourceRefs: []domain.SourceRef{
					{Kind: domain.SourceMnemosEvent, Locator: "evt-1"},
				},
			})
			if err != nil {
				t.Fatalf("extract: %v", err)
			}
			if len(drafts) != tc.want {
				t.Fatalf("got %d drafts, want %d", len(drafts), tc.want)
			}
			if tc.want > 0 {
				if drafts[0].OwnerID != tc.owner {
					t.Errorf("owner inheritance broken: %q", drafts[0].OwnerID)
				}
				if len(drafts[0].SourceRefs) == 0 || drafts[0].SourceRefs[len(drafts[0].SourceRefs)-1].Locator != "evt-1" {
					t.Errorf("source refs not propagated: %+v", drafts[0].SourceRefs)
				}
			}
		})
	}
}

func TestScriptedExtractor_OwnerOverride(t *testing.T) {
	t.Parallel()
	ext := llm.NewScriptedExtractor(llm.ScriptRule{
		Trigger: llm.ContainsAny("test"),
		Drafts: []domain.CommitmentDraft{{
			OwnerID:     "rule-owner",
			Description: "do thing",
			Confidence:  0.5,
		}},
	})
	drafts, err := ext.Extract(context.Background(), llm.ExtractInput{
		OwnerID: "input-owner",
		Text:    "test trigger",
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(drafts) != 1 {
		t.Fatalf("got %d drafts", len(drafts))
	}
	// Rule-supplied owner should win over input owner.
	if drafts[0].OwnerID != "rule-owner" {
		t.Errorf("expected rule-owner override, got %q", drafts[0].OwnerID)
	}
}
