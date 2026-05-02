package coordination

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/felixgeelhaar/bolt"
	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/ports"
)

type localPraxisStub struct{}

func (localPraxisStub) ListCapabilities(_ context.Context) ([]ports.Capability, error) {
	return nil, nil
}
func (localPraxisStub) DryRun(_ context.Context, _ domain.ActionRequest) (ports.SimulationResult, error) {
	return ports.SimulationResult{}, nil
}
func (localPraxisStub) Execute(_ context.Context, _ domain.ActionRequest) (ports.ExecutionResult, error) {
	return ports.ExecutionResult{}, nil
}

func TestAxiBudgetFromEnv_Defaults(t *testing.T) {
	t.Setenv("NOUS_AXI_MAX_DURATION", "")
	t.Setenv("NOUS_AXI_MAX_INVOCATIONS", "")
	b := axiBudgetFromEnv()
	if b.MaxDuration != 5*time.Minute {
		t.Errorf("MaxDuration = %v", b.MaxDuration)
	}
	if b.MaxCapabilityInvocations != 1000 {
		t.Errorf("MaxCapabilityInvocations = %d", b.MaxCapabilityInvocations)
	}
}

func TestAxiBudgetFromEnv_Overrides(t *testing.T) {
	t.Setenv("NOUS_AXI_MAX_DURATION", "30s")
	t.Setenv("NOUS_AXI_MAX_INVOCATIONS", "42")
	b := axiBudgetFromEnv()
	if b.MaxDuration != 30*time.Second {
		t.Errorf("MaxDuration = %v", b.MaxDuration)
	}
	if b.MaxCapabilityInvocations != 42 {
		t.Errorf("MaxCapabilityInvocations = %d", b.MaxCapabilityInvocations)
	}
}

func TestAxiBudgetFromEnv_RejectsBadValues(t *testing.T) {
	t.Setenv("NOUS_AXI_MAX_DURATION", "not-a-duration")
	t.Setenv("NOUS_AXI_MAX_INVOCATIONS", "-1")
	b := axiBudgetFromEnv()
	if b.MaxDuration != 5*time.Minute {
		t.Error("invalid duration should fall back to default")
	}
	if b.MaxCapabilityInvocations != 1000 {
		t.Error("non-positive invocations should fall back to default")
	}
}

func TestAxiKernel_KernelAccessor(t *testing.T) {
	t.Parallel()
	logger := bolt.New(bolt.NewJSONHandler(os.Stderr))
	k, err := NewAxiKernel(localPraxisStub{}, logger)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if k.Kernel() == nil {
		t.Error("kernel is nil")
	}
}

func TestBoltAxiLogger_AllLevels(t *testing.T) {
	t.Parallel()
	logger := bolt.New(bolt.NewJSONHandler(os.Stderr))
	l := boltAxiLogger{logger: logger}
	// All four log levels must not panic on empty fields.
	l.Debug("debug")
	l.Info("info")
	l.Warn("warn")
	l.Error("error")
}

func TestNewPlugin_NoErr(t *testing.T) {
	t.Parallel()
	p, err := newPlugin()
	if err != nil {
		t.Fatalf("newPlugin: %v", err)
	}
	contrib, err := p.Contribute()
	if err != nil {
		t.Fatalf("contribute: %v", err)
	}
	if contrib == nil {
		t.Fatal("contribution nil")
	}
}
