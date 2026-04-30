package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Decision is the durable record of "Nous chose X because Y". Every
// non-trivial branch in the pipeline writes one. Decisions are
// append-only: a wrong decision is corrected by a follow-up
// decision, never by mutation.
type Decision struct {
	ID          DecisionID
	Subject     string
	ContextRefs []SourceRef
	Inputs      DecisionInputs
	Outcome     DecisionOutcome
	Reason      string
	Confidence  float64
	CreatedAt   time.Time
}

// DecisionInputs captures the values that fed the decision so it can
// be replayed offline. Stored as JSON; structure is intentionally
// loose because every decision type has a different shape.
type DecisionInputs map[string]any

// DecisionOutcome captures what Nous actually decided. Same shape as
// inputs: free-form JSON keyed by the decision subject.
type DecisionOutcome map[string]any

// NewDecision constructs a decision with a fresh ID and the supplied
// clock. Reason and subject are required; everything else is
// optional but should be populated for forensic value.
func NewDecision(
	subject string,
	reason string,
	confidence float64,
	now time.Time,
) (Decision, error) {
	d := Decision{
		ID:         uuid.New(),
		Subject:    strings.TrimSpace(subject),
		Reason:     strings.TrimSpace(reason),
		Confidence: confidence,
		CreatedAt:  now.UTC(),
	}
	if err := d.Validate(); err != nil {
		return Decision{}, err
	}
	return d, nil
}

// Validate checks the minimum invariants. Subject and reason are
// required because a decision without a subject or rationale is
// indistinguishable from noise.
func (d Decision) Validate() error {
	if d.ID == uuid.Nil {
		return fmt.Errorf("%w: id is required", ErrValidation)
	}
	if d.Subject == "" {
		return fmt.Errorf("%w: subject is required", ErrValidation)
	}
	if d.Reason == "" {
		return fmt.Errorf("%w: reason is required", ErrValidation)
	}
	if d.Confidence < 0 || d.Confidence > 1 {
		return fmt.Errorf("%w: confidence must be in [0,1]", ErrValidation)
	}
	return nil
}
