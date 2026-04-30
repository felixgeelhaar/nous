package postgres

import (
	"context"
	"database/sql"

	"github.com/felixgeelhaar/nous/internal/ports"
)

// Conn bundles the per-aggregate repositories backed by the same
// *sql.DB. Mirrors the sqlite.Conn shape so the store factory can
// adapt either backend through the same dispatch.
type Conn struct {
	db *sql.DB

	Commitments   ports.CommitmentRepository
	Decisions     ports.DecisionRepository
	Interventions ports.InterventionRepository
	Goals         ports.GoalRepository
	Tasks         ports.TaskRepository
}

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

// Close releases the underlying connection pool.
func (c *Conn) Close() error { return c.db.Close() }

// Ping verifies the database connection is alive.
func (c *Conn) Ping(ctx context.Context) error { return c.db.PingContext(ctx) }
