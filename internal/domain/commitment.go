package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CommitmentStatus is the lifecycle of a tracked promise or
// obligation. Transitions are intentionally narrow: the spec defines
// five terminal/working states and `CanTransitionTo` is the only
// place new edges should be added.
type CommitmentStatus string

const (
	CommitmentPending    CommitmentStatus = "pending"
	CommitmentInProgress CommitmentStatus = "in_progress"
	CommitmentCompleted  CommitmentStatus = "completed"
	CommitmentMissed     CommitmentStatus = "missed"
	CommitmentCancelled  CommitmentStatus = "cancelled"
)

// IsActive reports whether the commitment is still being tracked by
// the evaluation worker. Terminal states are excluded.
func (s CommitmentStatus) IsActive() bool {
	switch s {
	case CommitmentPending, CommitmentInProgress:
		return true
	default:
		return false
	}
}

// Valid reports whether s is a known status string. Used at the
// repository boundary when reading raw rows.
func (s CommitmentStatus) Valid() bool {
	switch s {
	case CommitmentPending, CommitmentInProgress, CommitmentCompleted,
		CommitmentMissed, CommitmentCancelled:
		return true
	}
	return false
}

// CanTransitionTo enumerates legal status edges. Completed/missed/
// cancelled are terminal; pending and in_progress are the only
// origins. Worker code calls this before saving to surface bugs at
// the application layer rather than during a SQL constraint check.
func (s CommitmentStatus) CanTransitionTo(target CommitmentStatus) bool {
	if s == target {
		return true
	}
	switch s {
	case CommitmentPending:
		return target == CommitmentInProgress ||
			target == CommitmentCompleted ||
			target == CommitmentMissed ||
			target == CommitmentCancelled
	case CommitmentInProgress:
		return target == CommitmentCompleted ||
			target == CommitmentMissed ||
			target == CommitmentCancelled
	default:
		return false
	}
}

// Commitment is a promise, obligation, expected follow-up, or implied
// future responsibility. Confidence captures how sure Nous is the
// commitment is real (an LLM extractor sets this); RiskScore captures
// how likely it is to be missed (the risk engine maintains this).
type Commitment struct {
	ID              CommitmentID
	OwnerID         string
	Description     string
	SourceRefs      []SourceRef
	Entities        []EntityRef
	DueAt           *time.Time
	Status          CommitmentStatus
	Confidence      float64
	RiskScore       float64
	LastEvaluatedAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// NewCommitment constructs a fresh pending commitment with a random
// ID and timestamps clamped to the supplied clock. Inputs are
// validated; the zero-value pattern is intentionally avoided so
// callers cannot insert half-built rows.
func NewCommitment(
	ownerID string,
	description string,
	dueAt *time.Time,
	confidence float64,
	now time.Time,
) (Commitment, error) {
	c := Commitment{
		ID:          uuid.New(),
		OwnerID:     strings.TrimSpace(ownerID),
		Description: strings.TrimSpace(description),
		DueAt:       dueAt,
		Status:      CommitmentPending,
		Confidence:  confidence,
		CreatedAt:   now.UTC(),
		UpdatedAt:   now.UTC(),
	}
	if err := c.Validate(); err != nil {
		return Commitment{}, err
	}
	return c, nil
}

// Validate enforces invariants every Commitment must satisfy before
// persistence. Called by constructors and by repository writes
// defensively — a single source of truth for what a "good"
// commitment looks like.
func (c Commitment) Validate() error {
	if c.ID == uuid.Nil {
		return fmt.Errorf("%w: id is required", ErrValidation)
	}
	if c.OwnerID == "" {
		return fmt.Errorf("%w: owner_id is required", ErrValidation)
	}
	if c.Description == "" {
		return fmt.Errorf("%w: description is required", ErrValidation)
	}
	if c.Confidence < 0 || c.Confidence > 1 {
		return fmt.Errorf("%w: confidence must be in [0,1]", ErrValidation)
	}
	if c.RiskScore < 0 || c.RiskScore > 1 {
		return fmt.Errorf("%w: risk_score must be in [0,1]", ErrValidation)
	}
	if !c.Status.Valid() {
		return fmt.Errorf("%w: unknown status %q", ErrValidation, c.Status)
	}
	return nil
}

// Transition moves the commitment into a new status, updating
// UpdatedAt. Returns ErrInvalidTransition wrapped with both states
// so the worker log carries enough detail to debug surprises.
func (c *Commitment) Transition(target CommitmentStatus, now time.Time) error {
	if !c.Status.CanTransitionTo(target) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, c.Status, target)
	}
	c.Status = target
	c.UpdatedAt = now.UTC()
	return nil
}

// CommitmentDraft is the LLM extractor's output: a candidate
// commitment that has not yet earned a row in the database. The
// confidence threshold check happens after the draft is returned;
// drafts below threshold are dropped silently.
type CommitmentDraft struct {
	OwnerID     string
	Description string
	DueAt       *time.Time
	Entities    []EntityRef
	SourceRefs  []SourceRef
	Confidence  float64
}

// ToCommitment promotes a draft to a full commitment, asserting the
// validation invariants in one place. The clock is supplied so tests
// can pin timestamps deterministically.
func (d CommitmentDraft) ToCommitment(now time.Time) (Commitment, error) {
	c, err := NewCommitment(d.OwnerID, d.Description, d.DueAt, d.Confidence, now)
	if err != nil {
		return Commitment{}, err
	}
	c.Entities = append([]EntityRef(nil), d.Entities...)
	c.SourceRefs = append([]SourceRef(nil), d.SourceRefs...)
	return c, nil
}
