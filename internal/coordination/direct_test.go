package coordination_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/felixgeelhaar/nous/internal/coordination"
	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/ports"
	"github.com/google/uuid"
)

// stubPraxis is a record-and-replay fake. It captures every call so
// tests can verify what was sent to Praxis and returns canned
// responses keyed by capability name.
type stubPraxis struct {
	calls    []domain.ActionRequest
	response ports.ExecutionResult
	err      error
}

func (s *stubPraxis) ListCapabilities(_ context.Context) ([]ports.Capability, error) {
	return nil, nil
}
func (s *stubPraxis) DryRun(_ context.Context, req domain.ActionRequest) (ports.SimulationResult, error) {
	return ports.SimulationResult{Capability: req.Capability}, nil
}
func (s *stubPraxis) Execute(_ context.Context, req domain.ActionRequest) (ports.ExecutionResult, error) {
	s.calls = append(s.calls, req)
	if s.err != nil {
		return ports.ExecutionResult{}, s.err
	}
	res := s.response
	res.Capability = req.Capability
	res.IdempotencyKey = req.IdempotencyKey
	return res, nil
}

func TestDirectKernel_RequiresPraxis(t *testing.T) {
	t.Parallel()
	if _, err := coordination.NewDirectKernel(nil); err == nil {
		t.Error("expected error for nil praxis")
	}
}

func TestDirectKernel_NoSuggestedAction(t *testing.T) {
	t.Parallel()
	k, _ := coordination.NewDirectKernel(&stubPraxis{})
	commitmentID := uuid.New()
	iv, err := domain.NewIntervention(domain.InterventionNudge, "ping", &commitmentID, nil, nil, time.Now())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err = k.Execute(context.Background(), iv)
	if !errors.Is(err, coordination.ErrNoSuggestedAction) {
		t.Errorf("expected ErrNoSuggestedAction, got %v", err)
	}
}

func TestDirectKernel_HappyPath(t *testing.T) {
	t.Parallel()
	stub := &stubPraxis{response: ports.ExecutionResult{Success: true, ExecutedAt: time.Now()}}
	k, _ := coordination.NewDirectKernel(stub)
	commitmentID := uuid.New()
	action := &domain.ActionRequest{
		Capability:     "send_message",
		Payload:        map[string]any{"channel": "slack"},
		IdempotencyKey: "key-1",
	}
	iv, err := domain.NewIntervention(domain.InterventionAutomation, "ping", &commitmentID, nil, action, time.Now())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	out, err := k.Execute(context.Background(), iv)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !out.Success {
		t.Error("expected success")
	}
	if len(stub.calls) != 1 {
		t.Fatalf("expected 1 praxis call, got %d", len(stub.calls))
	}
	if stub.calls[0].IdempotencyKey != "key-1" {
		t.Errorf("idempotency key = %q, want key-1", stub.calls[0].IdempotencyKey)
	}
	if len(out.Evidence) != 1 || out.Evidence[0].Kind != "nous.action.executed" {
		t.Errorf("unexpected evidence: %+v", out.Evidence)
	}
}

func TestDirectKernel_DefaultIdempotencyKey(t *testing.T) {
	t.Parallel()
	stub := &stubPraxis{response: ports.ExecutionResult{Success: true}}
	k, _ := coordination.NewDirectKernel(stub)
	commitmentID := uuid.New()
	action := &domain.ActionRequest{Capability: "noop"} // no idempotency key
	iv, err := domain.NewIntervention(domain.InterventionAutomation, "ping", &commitmentID, nil, action, time.Now())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := k.Execute(context.Background(), iv); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stub.calls[0].IdempotencyKey != iv.ID.String() {
		t.Errorf("expected default idempotency key = intervention id, got %q", stub.calls[0].IdempotencyKey)
	}
}

func TestDirectKernel_PraxisError(t *testing.T) {
	t.Parallel()
	stub := &stubPraxis{err: errors.New("boom")}
	k, _ := coordination.NewDirectKernel(stub)
	commitmentID := uuid.New()
	action := &domain.ActionRequest{Capability: "noop"}
	iv, _ := domain.NewIntervention(domain.InterventionAutomation, "ping", &commitmentID, nil, action, time.Now())

	out, err := k.Execute(context.Background(), iv)
	if err == nil {
		t.Error("expected error from praxis to surface")
	}
	if out.Success {
		t.Error("expected outcome.Success=false on praxis error")
	}
	if len(out.Evidence) != 1 || out.Evidence[0].Kind != "nous.action.error" {
		t.Errorf("expected error evidence, got %+v", out.Evidence)
	}
}
