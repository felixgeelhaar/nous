package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// InterventionType classifies the Nous-side outcome shape. Nudges
// and suggestions surface to the user; escalations route to a
// different actor; automations bypass the user entirely (gated by
// the axi-go effect profile of the underlying action).
type InterventionType string

const (
	InterventionNudge      InterventionType = "nudge"
	InterventionSuggestion InterventionType = "suggestion"
	InterventionEscalation InterventionType = "escalation"
	InterventionAutomation InterventionType = "automation"
)

// Valid reports whether t is a known intervention type.
func (t InterventionType) Valid() bool {
	switch t {
	case InterventionNudge, InterventionSuggestion, InterventionEscalation, InterventionAutomation:
		return true
	}
	return false
}

// InterventionStatus tracks user response. Pending is the initial
// state; accepted/rejected are user actions; expired is set by the
// worker when a deadline elapses without a response; executed
// indicates the suggested action was carried out by Praxis.
type InterventionStatus string

const (
	InterventionPending  InterventionStatus = "pending"
	InterventionAccepted InterventionStatus = "accepted"
	InterventionRejected InterventionStatus = "rejected"
	InterventionExpired  InterventionStatus = "expired"
	InterventionExecuted InterventionStatus = "executed"
)

// Valid reports whether s is a known intervention status.
func (s InterventionStatus) Valid() bool {
	switch s {
	case InterventionPending, InterventionAccepted, InterventionRejected,
		InterventionExpired, InterventionExecuted:
		return true
	}
	return false
}

// CanTransitionTo enumerates legal status edges. Pending -> any;
// accepted -> executed (only); other terminal states are sticky.
func (s InterventionStatus) CanTransitionTo(target InterventionStatus) bool {
	if s == target {
		return true
	}
	switch s {
	case InterventionPending:
		return target == InterventionAccepted ||
			target == InterventionRejected ||
			target == InterventionExpired
	case InterventionAccepted:
		return target == InterventionExecuted
	default:
		return false
	}
}

// ActionRequest is what Nous hands to Praxis. The capability name
// and payload describe *what* to do; constraints describe how Praxis
// should behave (timeouts, idempotency expectations); the
// IdempotencyKey lets Praxis dedupe retries without ambiguity.
type ActionRequest struct {
	ID             uuid.UUID         `json:"id"`
	Capability     string            `json:"capability"`
	Payload        map[string]any    `json:"payload"`
	Constraints    []ActionConstraint `json:"constraints,omitempty"`
	IdempotencyKey string            `json:"idempotency_key"`
}

// ActionConstraint is a free-form key/value carrier. The set of
// recognised constraints is owned by Praxis; Nous only validates
// that the key is non-empty.
type ActionConstraint struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Intervention is a proactive message or action suggestion. It
// always points back at the commitment or task it is responding to;
// SuggestedAction is optional (a nudge has no action attached).
type Intervention struct {
	ID              InterventionID
	CommitmentID    *CommitmentID
	TaskID          *TaskID
	Type            InterventionType
	Message         string
	SuggestedAction *ActionRequest
	Status          InterventionStatus
	TriggeredAt     time.Time
	ResolvedAt      *time.Time
}

// NewIntervention constructs a pending intervention. At least one of
// CommitmentID or TaskID must be supplied so the audit trail back to
// the originating record is preserved.
func NewIntervention(
	t InterventionType,
	message string,
	commitmentID *CommitmentID,
	taskID *TaskID,
	suggested *ActionRequest,
	now time.Time,
) (Intervention, error) {
	iv := Intervention{
		ID:              uuid.New(),
		CommitmentID:    commitmentID,
		TaskID:          taskID,
		Type:            t,
		Message:         strings.TrimSpace(message),
		SuggestedAction: suggested,
		Status:          InterventionPending,
		TriggeredAt:     now.UTC(),
	}
	if err := iv.Validate(); err != nil {
		return Intervention{}, err
	}
	return iv, nil
}

// Validate enforces the minimum invariants.
func (iv Intervention) Validate() error {
	if iv.ID == uuid.Nil {
		return fmt.Errorf("%w: id is required", ErrValidation)
	}
	if !iv.Type.Valid() {
		return fmt.Errorf("%w: unknown type %q", ErrValidation, iv.Type)
	}
	if !iv.Status.Valid() {
		return fmt.Errorf("%w: unknown status %q", ErrValidation, iv.Status)
	}
	if iv.Message == "" {
		return fmt.Errorf("%w: message is required", ErrValidation)
	}
	if iv.CommitmentID == nil && iv.TaskID == nil {
		return fmt.Errorf("%w: commitment_id or task_id is required", ErrValidation)
	}
	return nil
}

// Resolve transitions the intervention to a terminal state and
// stamps ResolvedAt. Idempotent on the same target so worker retries
// don't double-resolve.
func (iv *Intervention) Resolve(target InterventionStatus, now time.Time) error {
	if !iv.Status.CanTransitionTo(target) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, iv.Status, target)
	}
	iv.Status = target
	t := now.UTC()
	iv.ResolvedAt = &t
	return nil
}
