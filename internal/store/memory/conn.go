// Package memory is the in-process backend for Nous repositories.
//
// Goals:
//   - serve as the test substrate for the application layer (every
//     pipeline test runs against this);
//   - expose the same port-typed surface as sqlite/postgres so
//     swap-by-config in store.Open is one line;
//   - never reach for shared global state — each Conn is independent.
//
// The store uses copy-on-read/write semantics so callers cannot
// mutate persisted records by hanging onto returned pointers. This
// matches the "rows are values" expectation the SQL backends will
// enforce at the driver layer.
package memory

import (
	"context"
	"sync"

	"github.com/felixgeelhaar/nous/internal/ports"
)

// Conn bundles the per-aggregate repositories that share this
// memory store's locks and maps. It mirrors the Conn shape Chronos
// uses so the store factory can return a uniform handle.
type Conn struct {
	Commitments   ports.CommitmentRepository
	Decisions     ports.DecisionRepository
	Interventions ports.InterventionRepository
	Goals         ports.GoalRepository
	Tasks         ports.TaskRepository
}

// New returns a fresh in-memory Conn. The repositories share a
// single mutex so cross-aggregate operations (e.g. saving a
// commitment + a decision in one logical step) cannot interleave
// inconsistently — at the cost of coarser locking, which is fine
// for a test substrate.
func New() *Conn {
	store := &state{
		mu:            sync.RWMutex{},
		commitments:   make(map[string]storedCommitment),
		decisions:     make(map[string]storedDecision),
		interventions: make(map[string]storedIntervention),
		goals:         make(map[string]storedGoal),
		tasks:         make(map[string]storedTask),
	}
	return &Conn{
		Commitments:   &commitmentRepo{state: store},
		Decisions:     &decisionRepo{state: store},
		Interventions: &interventionRepo{state: store},
		Goals:         &goalRepo{state: store},
		Tasks:         &taskRepo{state: store},
	}
}

// Close is a no-op; the in-memory backend has no resources to
// release. It exists so the store factory can call Close uniformly.
func (c *Conn) Close() error { return nil }

// Ping is a no-op; the in-memory backend is always reachable.
func (c *Conn) Ping(_ context.Context) error { return nil }
