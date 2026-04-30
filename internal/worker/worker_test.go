package worker_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/intervention"
	"github.com/felixgeelhaar/nous/internal/ports"
	"github.com/felixgeelhaar/nous/internal/pipeline"
	"github.com/felixgeelhaar/nous/internal/risk"
	"github.com/felixgeelhaar/nous/internal/store/memory"
	"github.com/felixgeelhaar/nous/internal/worker"
)

func TestWorker_TickAndStop(t *testing.T) {
	conn := memory.New()
	ctx := context.Background()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	overdue := now.Add(-30 * time.Minute)

	c, _ := domain.NewCommitment("u1", "send report", &overdue, 0.95, now)
	if err := conn.Commitments.Save(ctx, c); err != nil {
		t.Fatalf("save: %v", err)
	}

	eval, _ := pipeline.NewEvaluator(pipeline.EvaluatorConfig{
		Commitments:   conn.Commitments,
		Decisions:     conn.Decisions,
		Interventions: conn.Interventions,
		Risk:          risk.New(risk.DefaultConfig()),
		Intervention:  intervention.New(intervention.DefaultConfig()),
		Clock:         func() time.Time { return now },
	})

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	w := worker.New(eval, 100*time.Millisecond, logger)

	w.Start(ctx)

	// Wait for at least one tick to fire.
	time.Sleep(150 * time.Millisecond)

	w.Stop()

	// Verify something happened.
	ivs, _ := conn.Interventions.List(ctx, ports.InterventionFilter{})
	if len(ivs) == 0 {
		t.Error("expected at least one intervention after worker tick")
	}
}

func TestRunOnce(t *testing.T) {
	conn := memory.New()
	ctx := context.Background()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	overdue := now.Add(-30 * time.Minute)

	c, _ := domain.NewCommitment("u1", "send report", &overdue, 0.95, now)
	if err := conn.Commitments.Save(ctx, c); err != nil {
		t.Fatalf("save: %v", err)
	}

	eval, _ := pipeline.NewEvaluator(pipeline.EvaluatorConfig{
		Commitments:   conn.Commitments,
		Decisions:     conn.Decisions,
		Interventions: conn.Interventions,
		Risk:          risk.New(risk.DefaultConfig()),
		Intervention:  intervention.New(intervention.DefaultConfig()),
		Clock:         func() time.Time { return now },
	})

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if err := worker.RunOnce(ctx, eval, logger); err != nil {
		t.Fatalf("run once: %v", err)
	}

	ivs, _ := conn.Interventions.List(ctx, ports.InterventionFilter{})
	if len(ivs) == 0 {
		t.Error("expected at least one intervention")
	}
}
