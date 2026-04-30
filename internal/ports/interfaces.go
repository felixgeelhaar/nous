// Package ports declares the outbound interfaces ("ports") the Nous
// engine drives.
//
// Per-aggregate repositories follow the Interface Segregation
// Principle: each backend (memory, sqlite, postgres) implements only
// the slice it supports, and consumers depend on the narrowest
// interface they need.
//
// Outbound clients (Mnemos, Chronos, Praxis) are also declared here
// so swapping a real gRPC client for a fake in tests is a single
// constructor change.
package ports

import (
	"context"
	"errors"
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
)

// ErrClientNotConfigured is returned by outbound adapters when the
// backing service address was not provided at construction time.
var ErrClientNotConfigured = errors.New("outbound client not configured")

// CommitmentFilter is a structured query against the commitment
// store. All fields are optional; an empty filter matches every
// commitment in the database (use Limit to bound the result).
type CommitmentFilter struct {
	OwnerID  string
	Statuses []domain.CommitmentStatus
	DueBy    *time.Time // include only commitments due at or before
	Limit    int        // 0 = no limit
}

// CommitmentRepository persists and queries commitments. Save is
// upsert-shaped: callers do not need to know whether the row is new.
//
// Error contract:
//   - Get returns domain.ErrCommitmentNotFound when the id does not exist.
//   - UpdateRisk returns domain.ErrCommitmentNotFound when the id does not exist.
type CommitmentRepository interface {
	Save(ctx context.Context, c domain.Commitment) error
	Get(ctx context.Context, id domain.CommitmentID) (domain.Commitment, error)
	List(ctx context.Context, filter CommitmentFilter) ([]domain.Commitment, error)
	// FindActive returns the active backlog (pending + in_progress)
	// ordered by risk_score desc, due_at asc nulls last,
	// created_at asc — the standard worker scan order.
	FindActive(ctx context.Context, limit int) ([]domain.Commitment, error)
	UpdateRisk(ctx context.Context, id domain.CommitmentID, score float64, evaluatedAt time.Time) error
}

// DecisionRepository persists decisions. Decisions are append-only,
// so there is no Update method.
//
// Error contract:
//   - Get returns domain.ErrDecisionNotFound when the id does not exist.
type DecisionRepository interface {
	Save(ctx context.Context, d domain.Decision) error
	Get(ctx context.Context, id domain.DecisionID) (domain.Decision, error)
	// ListBySubject returns decisions matching the subject string,
	// most recent first. Used for "what did we decide about X?"
	// queries.
	ListBySubject(ctx context.Context, subject string, limit int) ([]domain.Decision, error)
}

// InterventionFilter is a structured query against the intervention
// store. Empty matches everything.
type InterventionFilter struct {
	CommitmentID *domain.CommitmentID
	TaskID       *domain.TaskID
	Statuses     []domain.InterventionStatus
	Since        *time.Time
	Limit        int
}

// InterventionRepository persists and queries interventions.
//
// Error contract:
//   - Get returns domain.ErrInterventionNotFound when the id does not exist.
type InterventionRepository interface {
	Save(ctx context.Context, iv domain.Intervention) error
	Get(ctx context.Context, id domain.InterventionID) (domain.Intervention, error)
	List(ctx context.Context, filter InterventionFilter) ([]domain.Intervention, error)
}

// GoalRepository persists goals.
//
// Error contract:
//   - Get returns domain.ErrGoalNotFound when the id does not exist.
type GoalRepository interface {
	Save(ctx context.Context, g domain.Goal) error
	Get(ctx context.Context, id domain.GoalID) (domain.Goal, error)
	ListByOwner(ctx context.Context, ownerID string, limit int) ([]domain.Goal, error)
}

// TaskFilter narrows task lookups.
type TaskFilter struct {
	GoalID       *domain.GoalID
	CommitmentID *domain.CommitmentID
	AssignedTo   *domain.AgentID
	Statuses     []domain.TaskStatus
	Limit        int
}

// TaskRepository persists tasks.
//
// Error contract:
//   - Get returns domain.ErrTaskNotFound when the id does not exist.
type TaskRepository interface {
	Save(ctx context.Context, t domain.Task) error
	Get(ctx context.Context, id domain.TaskID) (domain.Task, error)
	List(ctx context.Context, filter TaskFilter) ([]domain.Task, error)
}

// MnemosClient is the outbound port to Mnemos. Nous reads memories
// for context and appends events to record what happened.
type MnemosClient interface {
	// Recall returns memories relevant to the query. The shape of
	// "relevant" is owned by Mnemos; Nous only carries the result
	// references.
	Recall(ctx context.Context, q RecallQuery) ([]domain.MemoryRef, error)
	// AppendEvent records a Nous-originated fact in Mnemos so that
	// future recalls can use it as evidence. Returns the assigned
	// event id.
	AppendEvent(ctx context.Context, evt MnemosEvent) (string, error)
}

// RecallQuery parameterises a Mnemos lookup. Text and Entities are
// AND'd; Limit caps the response.
type RecallQuery struct {
	Text     string
	Entities []domain.EntityRef
	Since    *time.Time
	Limit    int
}

// MnemosEvent is the lean projection of an event Nous appends. The
// full Mnemos event schema lives over there; this struct is the
// transport-safe carrier.
type MnemosEvent struct {
	Kind       string         `json:"kind"`
	OccurredAt time.Time      `json:"occurred_at"`
	Actor      string         `json:"actor"`
	Subject    string         `json:"subject"`
	Body       map[string]any `json:"body"`
	SourceRefs []domain.SourceRef `json:"source_refs,omitempty"`
}

// ChronosClient is the outbound port to Chronos. Nous reads signals
// to inform risk evaluation.
type ChronosClient interface {
	GetSignals(ctx context.Context, filter SignalFilter) ([]domain.ChronosSignal, error)
}

// SignalFilter narrows a Chronos query. ScopeID is required by the
// Chronos service; the rest is optional.
type SignalFilter struct {
	ScopeID    string
	EntityRefs []domain.EntityRef
	Since      *time.Time
	Patterns   []string
	Limit      int
}

// PraxisClient is the outbound port to Praxis. Nous lists
// capabilities to know what it can ask for, dry-runs to validate a
// request before commitment, and executes once approved.
type PraxisClient interface {
	ListCapabilities(ctx context.Context) ([]Capability, error)
	DryRun(ctx context.Context, req domain.ActionRequest) (SimulationResult, error)
	Execute(ctx context.Context, req domain.ActionRequest) (ExecutionResult, error)
}

// Capability describes what Praxis can do.
type Capability struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

// SimulationResult is what DryRun returns: predicted side effects
// and any blockers Praxis would have refused on.
type SimulationResult struct {
	Capability  string   `json:"capability"`
	Predictions []string `json:"predictions"`
	Blockers    []string `json:"blockers"`
}

// ExecutionResult is what Execute returns: success/failure plus the
// raw output (capability-defined shape) and an idempotency token
// Praxis used.
type ExecutionResult struct {
	Capability     string         `json:"capability"`
	Success        bool           `json:"success"`
	Output         map[string]any `json:"output,omitempty"`
	Error          string         `json:"error,omitempty"`
	IdempotencyKey string         `json:"idempotency_key"`
	ExecutedAt     time.Time      `json:"executed_at"`
}
