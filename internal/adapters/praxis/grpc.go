// Package praxis is the gRPC adapter for Praxis (the action
// execution kernel). Praxis itself is not yet a deployed service;
// this adapter implements the ports.PraxisClient port so Nous's
// pipeline already has the seam wired up. When the Praxis service
// ships, only the proto wiring inside this file changes.
//
// Until then, the adapter degrades gracefully:
//
//   - With no `addr` configured, NewAdapter returns an adapter whose
//     methods all return ErrPraxisDisabled (caller-side: skip the
//     execute step rather than failing the whole pipeline).
//   - With `addr` configured but unreachable, the circuit breaker
//     trips and downstream calls fail fast.
package praxis

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/felixgeelhaar/nous/internal/circuit"
	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/ports"
	"github.com/google/uuid"
)

// ErrPraxisDisabled is returned when the adapter has no address
// configured. The pipeline should treat this as "execute is not
// available", not as an outage.
var ErrPraxisDisabled = errors.New("praxis: adapter disabled (no addr)")

// Adapter implements ports.PraxisClient. The exported surface is
// intentionally minimal — addresses, auth, and TLS arrive via Config.
type Adapter struct {
	cfg     Config
	breaker *circuit.Breaker

	mu     sync.RWMutex
	health string // "healthy" | "degraded" | "disabled"
}

// Config holds the wiring options for the adapter.
type Config struct {
	// Addr is the gRPC target. Empty disables the adapter.
	Addr string
	// TLSCertFile is an optional path to a CA cert.
	TLSCertFile string
	// BearerToken is an optional shared secret sent in the
	// `authorization` metadata header.
	BearerToken string
	// CallTimeout is the per-call deadline. Zero means no timeout.
	CallTimeout time.Duration
}

// NewAdapter constructs an adapter. With cfg.Addr empty, the adapter
// is "disabled" — calls return ErrPraxisDisabled and AdapterStatus
// reports "disabled".
func NewAdapter(cfg Config) *Adapter {
	a := &Adapter{cfg: cfg}
	if cfg.Addr == "" {
		a.health = "disabled"
		return a
	}
	a.breaker = circuit.New(circuit.Config{
		MaxFailures: 3,
	})
	a.health = "healthy"
	return a
}

// AdapterStatus reports the adapter's current health for the
// `/v1/health` aggregator.
func (a *Adapter) AdapterStatus() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.health
}

// ListCapabilities is the dry-run-friendly first call; consumers ask
// "what can you do?" before sending a real Execute. Until Praxis
// ships, this returns ErrPraxisDisabled.
func (a *Adapter) ListCapabilities(ctx context.Context) ([]ports.Capability, error) {
	if a.cfg.Addr == "" {
		return nil, ErrPraxisDisabled
	}
	// Real implementation will:
	//   conn, err := grpc.NewClient(a.cfg.Addr, ...)
	//   client := praxisv1.NewPraxisClient(conn)
	//   resp, err := client.ListCapabilities(ctx, &praxisv1.ListCapabilitiesRequest{})
	// and translate resp.Capabilities into []ports.Capability.
	return nil, fmt.Errorf("praxis: ListCapabilities: %w", errPraxisNotImplemented)
}

// DryRun simulates a request. Praxis is expected to validate the
// payload, confirm preconditions, and report blockers without making
// state changes.
func (a *Adapter) DryRun(ctx context.Context, req domain.ActionRequest) (ports.SimulationResult, error) {
	if a.cfg.Addr == "" {
		return ports.SimulationResult{}, ErrPraxisDisabled
	}
	return ports.SimulationResult{}, fmt.Errorf("praxis: DryRun(%s): %w", req.Capability, errPraxisNotImplemented)
}

// Execute runs the request. The IdempotencyKey on the request lets
// Praxis dedupe retries; this adapter generates one when absent so
// callers don't have to remember.
func (a *Adapter) Execute(ctx context.Context, req domain.ActionRequest) (ports.ExecutionResult, error) {
	if a.cfg.Addr == "" {
		return ports.ExecutionResult{}, ErrPraxisDisabled
	}
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = uuid.New().String()
	}
	return ports.ExecutionResult{}, fmt.Errorf("praxis: Execute(%s): %w", req.Capability, errPraxisNotImplemented)
}

// errPraxisNotImplemented is the placeholder error returned by every
// call until Praxis ships.
var errPraxisNotImplemented = errors.New("not implemented (Praxis service not yet deployed)")

// Compile-time interface satisfaction check.
var _ ports.PraxisClient = (*Adapter)(nil)
