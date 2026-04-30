// Package store wires the configured persistence backend into the
// Nous engine. Each backend (memory, sqlite, postgres) lives in a
// subpackage and exposes a Conn type bundling its repositories. This
// package's only job is to dispatch on the configured database type
// and return a uniform [Conn] handle the rest of the engine consumes.
package store

import (
	"context"
	"fmt"

	"github.com/felixgeelhaar/nous/internal/ports"
	"github.com/felixgeelhaar/nous/internal/store/memory"
	"github.com/felixgeelhaar/nous/internal/store/postgres"
	"github.com/felixgeelhaar/nous/internal/store/sqlite"
)

// Conn is the engine's view of a persistence backend: per-aggregate
// port-typed repositories plus a Close method. Backend packages
// return concrete Conn types from their packages; this struct
// adapts them into the port-typed view the rest of the engine
// consumes — so a constructor switch is the only place backend
// awareness lives.
type Conn struct {
	Commitments   ports.CommitmentRepository
	Decisions     ports.DecisionRepository
	Interventions ports.InterventionRepository
	Goals         ports.GoalRepository
	Tasks         ports.TaskRepository

	close func() error
	ping  func(context.Context) error
}

// Close releases backend resources. Repository pointers must not be
// used after Close returns.
func (c *Conn) Close() error {
	if c.close == nil {
		return nil
	}
	return c.close()
}

// Ping verifies the database connection is alive.
func (c *Conn) Ping(ctx context.Context) error {
	if c.ping == nil {
		return nil
	}
	return c.ping(ctx)
}

// Open returns a Conn for the configured backend. Supported types
// are "memory", "sqlite" / "sqlite3", and "postgres" / "postgresql".
// The connection string is interpreted by the backend driver; the
// memory backend ignores it.
//
// Open performs a connectivity check against external backends so
// configuration errors surface at startup rather than first use.
func Open(_ context.Context, dbType, connStr string) (*Conn, error) {
	switch dbType {
	case "memory":
		c := memory.New()
		return &Conn{
			Commitments:   c.Commitments,
			Decisions:     c.Decisions,
			Interventions: c.Interventions,
			Goals:         c.Goals,
			Tasks:         c.Tasks,
			close:         c.Close,
			ping:          c.Ping,
		}, nil
	case "sqlite", "sqlite3":
		c, err := sqlite.Open(connStr)
		if err != nil {
			return nil, err
		}
		return &Conn{
			Commitments:   c.Commitments,
			Decisions:     c.Decisions,
			Interventions: c.Interventions,
			Goals:         c.Goals,
			Tasks:         c.Tasks,
			close:         c.Close,
			ping:          c.Ping,
		}, nil
	case "postgres", "postgresql":
		c, err := postgres.Open(connStr)
		if err != nil {
			return nil, err
		}
		return &Conn{
			Commitments:   c.Commitments,
			Decisions:     c.Decisions,
			Interventions: c.Interventions,
			Goals:         c.Goals,
			Tasks:         c.Tasks,
			close:         c.Close,
			ping:          c.Ping,
		}, nil
	default:
		return nil, fmt.Errorf("store: unsupported database type %q (supported: %v)",
			dbType, SupportedTypes())
	}
}

// SupportedTypes lists the backends this build understands.
func SupportedTypes() []string {
	return []string{"memory", "sqlite", "postgres"}
}
