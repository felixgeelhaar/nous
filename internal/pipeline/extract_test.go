package pipeline_test

import (
	"context"
	"testing"
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/llm"
	"github.com/felixgeelhaar/nous/internal/pipeline"
	"github.com/felixgeelhaar/nous/internal/store/memory"
)

func TestExtractor_Extract(t *testing.T) {
	t.Parallel()
	conn := memory.New()
	ctx := context.Background()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	due := now.Add(24 * time.Hour)

	scripted := llm.NewScriptedExtractor(llm.ScriptRule{
		Trigger: llm.ContainsAny("follow up"),
		Drafts: []domain.CommitmentDraft{
			{Description: "follow up with alex", DueAt: &due, Confidence: 0.92},
			{Description: "low confidence", Confidence: 0.4}, // below threshold
		},
	})

	ext, err := pipeline.NewExtractor(pipeline.ExtractorConfig{
		Commitments:   conn.Commitments,
		Decisions:     conn.Decisions,
		Extractor:     scripted,
		MinConfidence: 0.6,
		Clock:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	res, err := ext.Extract(ctx, llm.ExtractInput{
		OwnerID:    "u1",
		Text:       "I'll follow up with Alex",
		SourceRefs: []domain.SourceRef{{Kind: domain.SourceMnemosEvent, Locator: "evt-1"}},
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	if res.Considered != 2 {
		t.Errorf("considered = %d, want 2", res.Considered)
	}
	if len(res.Saved) != 1 {
		t.Fatalf("expected 1 saved, got %d", len(res.Saved))
	}
	if res.Dropped != 1 {
		t.Errorf("dropped = %d, want 1", res.Dropped)
	}
	if res.DecisionID == nil {
		t.Error("expected decision row to be persisted")
	}

	got, err := conn.Commitments.Get(ctx, res.Saved[0])
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.OwnerID != "u1" {
		t.Errorf("owner = %q, want u1", got.OwnerID)
	}
	if len(got.SourceRefs) == 0 {
		t.Error("expected source_refs to flow through")
	}
}

func TestExtractor_NoMatchIsNoop(t *testing.T) {
	t.Parallel()
	conn := memory.New()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	ext, err := pipeline.NewExtractor(pipeline.ExtractorConfig{
		Commitments:   conn.Commitments,
		Decisions:     conn.Decisions,
		Extractor:     llm.NewScriptedExtractor(),
		MinConfidence: 0.5,
		Clock:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	res, err := ext.Extract(context.Background(), llm.ExtractInput{Text: "nothing here"})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if res.Considered != 0 || len(res.Saved) != 0 {
		t.Errorf("expected empty result, got %+v", res)
	}
}

func TestExtractor_NewValidates(t *testing.T) {
	t.Parallel()
	conn := memory.New()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		cfg  pipeline.ExtractorConfig
	}{
		{"nil commitments", pipeline.ExtractorConfig{Decisions: conn.Decisions, Extractor: llm.NewScriptedExtractor(), MinConfidence: 0.5, Clock: func() time.Time { return now }}},
		{"nil decisions", pipeline.ExtractorConfig{Commitments: conn.Commitments, Extractor: llm.NewScriptedExtractor(), MinConfidence: 0.5, Clock: func() time.Time { return now }}},
		{"nil extractor", pipeline.ExtractorConfig{Commitments: conn.Commitments, Decisions: conn.Decisions, MinConfidence: 0.5, Clock: func() time.Time { return now }}},
		{"bad confidence", pipeline.ExtractorConfig{Commitments: conn.Commitments, Decisions: conn.Decisions, Extractor: llm.NewScriptedExtractor(), MinConfidence: 1.5, Clock: func() time.Time { return now }}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pipeline.NewExtractor(tc.cfg)
			if err == nil {
				t.Error("expected error")
			}
		})
	}
}
