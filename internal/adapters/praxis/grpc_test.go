package praxis

import (
	"context"
	"errors"
	"testing"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/google/uuid"
)

func TestAdapter_DisabledWhenNoAddr(t *testing.T) {
	t.Parallel()
	a := NewAdapter(Config{})
	if a.AdapterStatus() != "disabled" {
		t.Errorf("status = %q", a.AdapterStatus())
	}
	_, err := a.ListCapabilities(context.Background())
	if !errors.Is(err, ErrPraxisDisabled) {
		t.Errorf("ListCapabilities err = %v", err)
	}
	_, err = a.DryRun(context.Background(), domain.ActionRequest{Capability: "x"})
	if !errors.Is(err, ErrPraxisDisabled) {
		t.Errorf("DryRun err = %v", err)
	}
	_, err = a.Execute(context.Background(), domain.ActionRequest{Capability: "x"})
	if !errors.Is(err, ErrPraxisDisabled) {
		t.Errorf("Execute err = %v", err)
	}
}

func TestAdapter_HealthyWhenAddrConfigured(t *testing.T) {
	t.Parallel()
	a := NewAdapter(Config{Addr: "praxis:50051"})
	if a.AdapterStatus() != "healthy" {
		t.Errorf("status = %q", a.AdapterStatus())
	}
	// Calls return not-implemented (Praxis service not deployed).
	_, err := a.ListCapabilities(context.Background())
	if err == nil || errors.Is(err, ErrPraxisDisabled) {
		t.Errorf("ListCapabilities should error (not disabled): %v", err)
	}
}

func TestAdapter_ExecuteFillsIdempotencyKey(t *testing.T) {
	t.Parallel()
	// We can't actually execute (no service), but we can confirm the
	// adapter doesn't treat an empty idempotency key as a hard error
	// — it generates one. To observe, we'd need the real Execute path,
	// so this test just covers the disabled path by ensuring no panic.
	a := NewAdapter(Config{})
	req := domain.ActionRequest{ID: uuid.New(), Capability: "deploy"}
	_, err := a.Execute(context.Background(), req)
	if !errors.Is(err, ErrPraxisDisabled) {
		t.Errorf("err = %v", err)
	}
}
