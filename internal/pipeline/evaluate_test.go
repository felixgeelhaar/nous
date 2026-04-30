package pipeline_test

import (
	"context"
	"testing"
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/intervention"
	"github.com/felixgeelhaar/nous/internal/pipeline"
	"github.com/felixgeelhaar/nous/internal/ports"
	"github.com/felixgeelhaar/nous/internal/risk"
	"github.com/felixgeelhaar/nous/internal/store/memory"
)

// stubChronos records what was asked and returns a canned signal
// list. Lets the test assert the pipeline actually queries Chronos.
type stubChronos struct {
	calls    int
	response []domain.ChronosSignal
	err      error
}

func (s *stubChronos) GetSignals(_ context.Context, _ ports.SignalFilter) ([]domain.ChronosSignal, error) {
	s.calls++
	return s.response, s.err
}

// stubMnemos returns canned memories. Used to verify Recall is wired
// even though MVP risk doesn't weight memories yet.
type stubMnemos struct {
	memories []domain.MemoryRef
	events   []ports.MnemosEvent
}

func (s *stubMnemos) Recall(_ context.Context, _ ports.RecallQuery) ([]domain.MemoryRef, error) {
	return s.memories, nil
}

func (s *stubMnemos) AppendEvent(_ context.Context, evt ports.MnemosEvent) (string, error) {
	s.events = append(s.events, evt)
	return "evt-1", nil
}

func TestEvaluator_FiresInterventionForOverdueCommitment(t *testing.T) {
	t.Parallel()
	conn := memory.New()
	ctx := context.Background()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	overdue := now.Add(-30 * time.Minute)
	farFuture := now.Add(72 * time.Hour)

	overdueC, err := domain.NewCommitment("u1", "send Q2 report", &overdue, 0.95, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := conn.Commitments.Save(ctx, overdueC); err != nil {
		t.Fatalf("save: %v", err)
	}

	calmC, err := domain.NewCommitment("u1", "background prep", &farFuture, 0.5, now)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := conn.Commitments.Save(ctx, calmC); err != nil {
		t.Fatalf("save: %v", err)
	}

	chronos := &stubChronos{}
	mnemos := &stubMnemos{}
	eval, err := pipeline.NewEvaluator(pipeline.EvaluatorConfig{
		Commitments:   conn.Commitments,
		Decisions:     conn.Decisions,
		Interventions: conn.Interventions,
		Mnemos:        mnemos,
		Chronos:       chronos,
		Risk:          risk.New(risk.DefaultConfig()),
		Intervention:  intervention.New(intervention.DefaultConfig()),
		Clock:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new evaluator: %v", err)
	}

	res, err := eval.Evaluate(ctx, pipeline.EvaluateOptions{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	if res.Evaluated != 2 {
		t.Errorf("evaluated = %d, want 2", res.Evaluated)
	}
	if res.Updated != 2 {
		t.Errorf("updated = %d, want 2", res.Updated)
	}
	if len(res.Interventions) != 1 {
		t.Fatalf("expected 1 intervention, got %d", len(res.Interventions))
	}
	if len(res.Decisions) != 2 {
		t.Errorf("expected 2 decisions (one per commitment), got %d", len(res.Decisions))
	}

	// Verify the intervention is anchored to the overdue commitment.
	iv, err := conn.Interventions.Get(ctx, res.Interventions[0])
	if err != nil {
		t.Fatalf("get intervention: %v", err)
	}
	if iv.CommitmentID == nil || *iv.CommitmentID != overdueC.ID {
		t.Errorf("intervention anchored on %v, want %v", iv.CommitmentID, overdueC.ID)
	}

	// Verify the risk score landed on the persisted commitment.
	persisted, err := conn.Commitments.Get(ctx, overdueC.ID)
	if err != nil {
		t.Fatalf("get commitment: %v", err)
	}
	if persisted.RiskScore < 0.6 {
		t.Errorf("expected high risk after evaluate, got %v", persisted.RiskScore)
	}
	if persisted.LastEvaluatedAt == nil {
		t.Error("expected last_evaluated_at to be set")
	}

	if chronos.calls != 2 {
		t.Errorf("chronos called %d times, want 2", chronos.calls)
	}
	if len(mnemos.events) != 2 {
		t.Errorf("mnemos AppendEvent called %d times, want 2", len(mnemos.events))
	}
}

func TestEvaluator_NoActiveCommitmentsIsNoop(t *testing.T) {
	t.Parallel()
	conn := memory.New()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	eval, err := pipeline.NewEvaluator(pipeline.EvaluatorConfig{
		Commitments:   conn.Commitments,
		Decisions:     conn.Decisions,
		Interventions: conn.Interventions,
		Risk:          risk.New(risk.DefaultConfig()),
		Intervention:  intervention.New(intervention.DefaultConfig()),
		Clock:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new evaluator: %v", err)
	}
	res, err := eval.Evaluate(context.Background(), pipeline.EvaluateOptions{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if res.Evaluated != 0 || res.Updated != 0 {
		t.Errorf("expected empty result, got %+v", res)
	}
}

func TestEvaluator_RequiresCoreDeps(t *testing.T) {
	t.Parallel()
	_, err := pipeline.NewEvaluator(pipeline.EvaluatorConfig{})
	if err == nil {
		t.Error("expected error for empty config")
	}
}

func TestEvaluator_SoftFailOnChronosError(t *testing.T) {
	t.Parallel()
	conn := memory.New()
	ctx := context.Background()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	overdue := now.Add(-30 * time.Minute)

	c, _ := domain.NewCommitment("u1", "send report", &overdue, 0.95, now)
	if err := conn.Commitments.Save(ctx, c); err != nil {
		t.Fatalf("save: %v", err)
	}

	chronos := &stubChronos{err: context.DeadlineExceeded}
	eval, _ := pipeline.NewEvaluator(pipeline.EvaluatorConfig{
		Commitments:   conn.Commitments,
		Decisions:     conn.Decisions,
		Interventions: conn.Interventions,
		Chronos:       chronos,
		Risk:          risk.New(risk.DefaultConfig()),
		Intervention:  intervention.New(intervention.DefaultConfig()),
		Clock:         func() time.Time { return now },
	})

	res, err := eval.Evaluate(ctx, pipeline.EvaluateOptions{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if res.Evaluated != 1 || res.Updated != 1 {
		t.Errorf("expected 1 evaluated/updated, got %+v", res)
	}
	// Should still fire intervention even without signals.
	if len(res.Interventions) != 1 {
		t.Errorf("expected 1 intervention, got %d", len(res.Interventions))
	}
}

func TestEvaluator_Limit(t *testing.T) {
	t.Parallel()
	conn := memory.New()
	ctx := context.Background()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 5; i++ {
		c, _ := domain.NewCommitment("u1", "task", nil, 0.9, now.Add(time.Duration(i)*time.Second))
		if err := conn.Commitments.Save(ctx, c); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	eval, _ := pipeline.NewEvaluator(pipeline.EvaluatorConfig{
		Commitments:   conn.Commitments,
		Decisions:     conn.Decisions,
		Interventions: conn.Interventions,
		Risk:          risk.New(risk.DefaultConfig()),
		Intervention:  intervention.New(intervention.DefaultConfig()),
		Clock:         func() time.Time { return now },
	})

	res, err := eval.Evaluate(ctx, pipeline.EvaluateOptions{Limit: 2})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if res.Evaluated != 2 {
		t.Errorf("evaluated = %d, want 2", res.Evaluated)
	}
}
