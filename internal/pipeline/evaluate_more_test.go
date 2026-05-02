package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/intervention"
	"github.com/felixgeelhaar/nous/internal/ports"
	"github.com/felixgeelhaar/nous/internal/risk"
	"github.com/felixgeelhaar/nous/internal/store/memory"
)

func TestSystemClock_ReturnsUTC(t *testing.T) {
	t.Parallel()
	got := SystemClock()
	if got.Location() != time.UTC {
		t.Errorf("not UTC: %v", got.Location())
	}
}

func TestRiskLabel(t *testing.T) {
	t.Parallel()
	cases := map[float64]string{
		0.0:  "low",
		0.39: "low",
		0.4:  "medium",
		0.69: "medium",
		0.7:  "high",
		1.0:  "high",
	}
	for score, want := range cases {
		if got := riskLabel(score); got != want {
			t.Errorf("riskLabel(%v) = %q, want %q", score, got, want)
		}
	}
}

// stubPraxis lets us drive Evaluator.praxisDryRun in both nil and
// non-nil branches.
type stubPraxis struct {
	calls int
	err   error
}

func (p *stubPraxis) ListCapabilities(_ context.Context) ([]ports.Capability, error) {
	return nil, p.err
}
func (p *stubPraxis) DryRun(_ context.Context, _ domain.ActionRequest) (ports.SimulationResult, error) {
	p.calls++
	return ports.SimulationResult{}, p.err
}
func (p *stubPraxis) Execute(_ context.Context, _ domain.ActionRequest) (ports.ExecutionResult, error) {
	return ports.ExecutionResult{}, p.err
}

func TestEvaluator_PraxisDryRun_NilSkips(t *testing.T) {
	t.Parallel()
	mem := memory.New()
	e, err := NewEvaluator(EvaluatorConfig{
		Commitments:   mem.Commitments,
		Decisions:     mem.Decisions,
		Interventions: mem.Interventions,
		Risk:          risk.New(risk.DefaultConfig()),
		Intervention:  intervention.New(intervention.DefaultConfig()),
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	// No praxis wired — call should no-op.
	e.praxisDryRun(context.Background(), domain.Commitment{}, domain.Intervention{})
}

func TestEvaluator_PraxisDryRun_CallsConfiguredClient(t *testing.T) {
	t.Parallel()
	mem := memory.New()
	stub := &stubPraxis{}
	e, err := NewEvaluator(EvaluatorConfig{
		Commitments:   mem.Commitments,
		Decisions:     mem.Decisions,
		Interventions: mem.Interventions,
		Praxis:        stub,
		Risk:          risk.New(risk.DefaultConfig()),
		Intervention:  intervention.New(intervention.DefaultConfig()),
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	now := time.Now().UTC()
	c, _ := domain.NewCommitment("u1", "ship", nil, 0.9, now)
	iv, _ := domain.NewIntervention(domain.InterventionAutomation, "auto", &c.ID, nil, nil, now)
	e.praxisDryRun(context.Background(), c, iv)
	if stub.calls != 1 {
		t.Errorf("calls = %d, want 1", stub.calls)
	}
}

func TestEvaluator_PraxisDryRun_SoftFailOnError(t *testing.T) {
	t.Parallel()
	mem := memory.New()
	stub := &stubPraxis{err: errors.New("praxis down")}
	e, _ := NewEvaluator(EvaluatorConfig{
		Commitments:   mem.Commitments,
		Decisions:     mem.Decisions,
		Interventions: mem.Interventions,
		Praxis:        stub,
		Risk:          risk.New(risk.DefaultConfig()),
		Intervention:  intervention.New(intervention.DefaultConfig()),
	})
	now := time.Now().UTC()
	c, _ := domain.NewCommitment("u1", "ship", nil, 0.9, now)
	iv, _ := domain.NewIntervention(domain.InterventionAutomation, "auto", &c.ID, nil, nil, now)
	// Must not panic / return — soft-fail is the contract.
	e.praxisDryRun(context.Background(), c, iv)
	if stub.calls != 1 {
		t.Errorf("calls = %d", stub.calls)
	}
}
