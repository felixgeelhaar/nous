package coordination_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/felixgeelhaar/bolt"
	"github.com/felixgeelhaar/nous/internal/coordination"
	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/ports"
	"github.com/google/uuid"
)

func TestAxiKernel_RequiresPraxis(t *testing.T) {
	t.Parallel()
	logger := bolt.New(bolt.NewJSONHandler(os.Stderr))
	if _, err := coordination.NewAxiKernel(nil, logger); err == nil {
		t.Error("expected error for nil praxis")
	}
}

func TestAxiKernel_RequiresLogger(t *testing.T) {
	t.Parallel()
	if _, err := coordination.NewAxiKernel(&stubPraxis{}, nil); err == nil {
		t.Error("expected error for nil logger")
	}
}

func TestAxiKernel_HappyPath(t *testing.T) {
	t.Parallel()
	stub := &stubPraxis{response: ports.ExecutionResult{
		Success:    true,
		Output:     map[string]any{"sent": true},
		ExecutedAt: time.Now(),
	}}
	logger := bolt.New(bolt.NewJSONHandler(os.Stderr))
	k, err := coordination.NewAxiKernel(stub, logger)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	commitmentID := uuid.New()
	action := &domain.ActionRequest{
		Capability:     "send_message",
		Payload:        map[string]any{"channel": "slack", "to": "alex"},
		IdempotencyKey: "iv-key",
	}
	iv, err := domain.NewIntervention(domain.InterventionAutomation, "ping alex", &commitmentID, nil, action, time.Now())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	out, err := k.Execute(context.Background(), iv)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !out.Success {
		t.Errorf("expected success, got %+v", out)
	}
	if len(stub.calls) != 1 {
		t.Fatalf("expected 1 praxis call, got %d", len(stub.calls))
	}
	if stub.calls[0].IdempotencyKey != "iv-key" {
		t.Errorf("idempotency key = %q, want iv-key", stub.calls[0].IdempotencyKey)
	}
	if stub.calls[0].Capability != "send_message" {
		t.Errorf("capability not preserved: %q", stub.calls[0].Capability)
	}
	if len(out.Evidence) == 0 {
		t.Error("expected evidence chain to be populated")
	}
}

func TestAxiKernel_PraxisError_SurfacesAsFailure(t *testing.T) {
	t.Parallel()
	stub := &stubPraxis{err: errors.New("network down")}
	logger := bolt.New(bolt.NewJSONHandler(os.Stderr))
	k, err := coordination.NewAxiKernel(stub, logger)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	commitmentID := uuid.New()
	action := &domain.ActionRequest{Capability: "noop"}
	iv, _ := domain.NewIntervention(domain.InterventionAutomation, "ping", &commitmentID, nil, action, time.Now())

	out, _ := k.Execute(context.Background(), iv)
	if out.Success {
		t.Error("expected Success=false on praxis error")
	}
	if out.ErrorMessage == "" {
		t.Error("expected ErrorMessage to be populated")
	}
}

func TestAxiKernel_NoSuggestedAction(t *testing.T) {
	t.Parallel()
	logger := bolt.New(bolt.NewJSONHandler(os.Stderr))
	k, err := coordination.NewAxiKernel(&stubPraxis{}, logger)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	commitmentID := uuid.New()
	iv, _ := domain.NewIntervention(domain.InterventionNudge, "ping", &commitmentID, nil, nil, time.Now())
	if _, err := k.Execute(context.Background(), iv); !errors.Is(err, coordination.ErrNoSuggestedAction) {
		t.Errorf("expected ErrNoSuggestedAction, got %v", err)
	}
}
