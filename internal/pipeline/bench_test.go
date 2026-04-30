package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/intervention"
	"github.com/felixgeelhaar/nous/internal/llm"
	"github.com/felixgeelhaar/nous/internal/risk"
	"github.com/felixgeelhaar/nous/internal/store/memory"
)

func BenchmarkExtractor_Extract(b *testing.B) {
	ctx := context.Background()
	store := memory.New()
	extractor, err := NewExtractor(ExtractorConfig{
		Commitments:   store.Commitments,
		Decisions:     store.Decisions,
		Extractor:     llm.NewScriptedExtractor(),
		MinConfidence: 0.0,
	})
	if err != nil {
		b.Fatal(err)
	}

	in := llm.ExtractInput{
		OwnerID: "alice",
		Text:    "I will finish the report by Friday and call the client tomorrow.",
		Hints:   []string{"work", "deadline"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = extractor.Extract(ctx, in)
	}
}

func BenchmarkEvaluator_Evaluate(b *testing.B) {
	ctx := context.Background()
	store := memory.New()
	riskEngine := risk.New(risk.DefaultConfig())
	ivEngine := intervention.New(intervention.DefaultConfig())

	// Seed commitments
	now := time.Now().UTC()
	for i := 0; i < 100; i++ {
		due := now.Add(time.Duration(i) * time.Hour)
		c, _ := domain.NewCommitment("owner", "task", &due, 0.8, now)
		_ = store.Commitments.Save(ctx, c)
	}

	eval, err := NewEvaluator(EvaluatorConfig{
		Commitments:   store.Commitments,
		Decisions:     store.Decisions,
		Interventions: store.Interventions,
		Risk:          riskEngine,
		Intervention:  ivEngine,
	})
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = eval.Evaluate(ctx, EvaluateOptions{})
	}
}

func BenchmarkEvaluator_Evaluate_Limit10(b *testing.B) {
	ctx := context.Background()
	store := memory.New()
	riskEngine := risk.New(risk.DefaultConfig())
	ivEngine := intervention.New(intervention.DefaultConfig())

	now := time.Now().UTC()
	for i := 0; i < 100; i++ {
		due := now.Add(time.Duration(i) * time.Hour)
		c, _ := domain.NewCommitment("owner", "task", &due, 0.8, now)
		_ = store.Commitments.Save(ctx, c)
	}

	eval, err := NewEvaluator(EvaluatorConfig{
		Commitments:   store.Commitments,
		Decisions:     store.Decisions,
		Interventions: store.Interventions,
		Risk:          riskEngine,
		Intervention:  ivEngine,
	})
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = eval.Evaluate(ctx, EvaluateOptions{Limit: 10})
	}
}
