package sqlite

import (
	"context"
	"database/sql"

	"github.com/felixgeelhaar/nous/internal/ports"
)

// Conn bundles the per-aggregate repositories that share a single
// *sql.DB. The store factory adapts this to the engine-facing
// store.Conn type.
type Conn struct {
	db *sql.DB

	Commitments   ports.CommitmentRepository
	Decisions     ports.DecisionRepository
	Interventions ports.InterventionRepository
	Goals         ports.GoalRepository
	Tasks         ports.TaskRepository
}

// newConn wires the repositories around a ready-to-use *sql.DB.
// Kept private so callers always go through Open which guarantees
// migrations have run.
func newConn(db *sql.DB) *Conn {
	return &Conn{
		db:            db,
		Commitments:   &commitmentRepo{db: db},
		Decisions:     &decisionRepo{db: db},
		Interventions: &interventionRepo{db: db},
		Goals:         &goalRepo{db: db},
		Tasks:         &taskRepo{db: db},
	}
}

// Close releases the underlying database connection pool.
func (c *Conn) Close() error { return c.db.Close() }

// Ping verifies the database connection is alive.
func (c *Conn) Ping(ctx context.Context) error { return c.db.PingContext(ctx) }
