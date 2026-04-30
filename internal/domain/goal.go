package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// GoalStatus tracks a goal through planning and execution. Open is
// the inbox; planned has a Plan attached; active means tasks are in
// flight; the rest are terminal.
type GoalStatus string

const (
	GoalOpen      GoalStatus = "open"
	GoalPlanned   GoalStatus = "planned"
	GoalActive    GoalStatus = "active"
	GoalCompleted GoalStatus = "completed"
	GoalCancelled GoalStatus = "cancelled"
)

// Valid reports whether s is a known goal status.
func (s GoalStatus) Valid() bool {
	switch s {
	case GoalOpen, GoalPlanned, GoalActive, GoalCompleted, GoalCancelled:
		return true
	}
	return false
}

// CanTransitionTo enumerates legal status edges. Completed and
// cancelled are terminal; open and planned are origins.
func (s GoalStatus) CanTransitionTo(target GoalStatus) bool {
	if s == target {
		return true
	}
	switch s {
	case GoalOpen:
		return target == GoalPlanned || target == GoalCancelled
	case GoalPlanned:
		return target == GoalActive || target == GoalCancelled
	case GoalActive:
		return target == GoalCompleted || target == GoalCancelled
	default:
		return false
	}
}

// Goal is a high-level objective the user (or another goal) wants
// pursued. Plans turn goals into PlanSteps; PlanSteps turn into
// Tasks. Goals themselves never carry execution detail.
type Goal struct {
	ID          GoalID
	OwnerID     string
	Title       string
	Description string
	Priority    Priority
	Status      GoalStatus
	SourceRefs  []SourceRef
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// NewGoal constructs a fresh open goal.
func NewGoal(
	ownerID string,
	title string,
	description string,
	priority Priority,
	now time.Time,
) (Goal, error) {
	g := Goal{
		ID:          uuid.New(),
		OwnerID:     strings.TrimSpace(ownerID),
		Title:       strings.TrimSpace(title),
		Description: strings.TrimSpace(description),
		Priority:    priority,
		Status:      GoalOpen,
		CreatedAt:   now.UTC(),
		UpdatedAt:   now.UTC(),
	}
	if err := g.Validate(); err != nil {
		return Goal{}, err
	}
	return g, nil
}

// Validate enforces the minimum invariants.
func (g Goal) Validate() error {
	if g.ID == uuid.Nil {
		return fmt.Errorf("%w: id is required", ErrValidation)
	}
	if g.OwnerID == "" {
		return fmt.Errorf("%w: owner_id is required", ErrValidation)
	}
	if g.Title == "" {
		return fmt.Errorf("%w: title is required", ErrValidation)
	}
	if !g.Status.Valid() {
		return fmt.Errorf("%w: unknown status %q", ErrValidation, g.Status)
	}
	return nil
}

// Transition moves the goal into a new status, updating UpdatedAt.
func (g *Goal) Transition(target GoalStatus, now time.Time) error {
	if !g.Status.CanTransitionTo(target) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, g.Status, target)
	}
	g.Status = target
	g.UpdatedAt = now.UTC()
	return nil
}

// Plan is a structured approach for reaching a Goal. The Steps
// ordering is significant: dependencies are expressed via Step.DependsOn,
// but tools that want a default execution order can read Steps in
// slice order.
type Plan struct {
	ID        PlanID
	GoalID    GoalID
	Status    PlanStatus
	Steps     []PlanStep
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PlanStatus is the plan-level lifecycle. A plan in `superseded`
// remains for audit but is not the active plan for its goal.
type PlanStatus string

const (
	PlanDraft      PlanStatus = "draft"
	PlanActive     PlanStatus = "active"
	PlanCompleted  PlanStatus = "completed"
	PlanSuperseded PlanStatus = "superseded"
)

// Valid reports whether s is a known plan status.
func (s PlanStatus) Valid() bool {
	switch s {
	case PlanDraft, PlanActive, PlanCompleted, PlanSuperseded:
		return true
	}
	return false
}

// CanTransitionTo enumerates legal status edges.
func (s PlanStatus) CanTransitionTo(target PlanStatus) bool {
	if s == target {
		return true
	}
	switch s {
	case PlanDraft:
		return target == PlanActive || target == PlanSuperseded
	case PlanActive:
		return target == PlanCompleted || target == PlanSuperseded
	default:
		return false
	}
}

// Validate enforces the minimum invariants for a Plan.
func (p Plan) Validate() error {
	if p.ID == uuid.Nil {
		return fmt.Errorf("%w: id is required", ErrValidation)
	}
	if p.GoalID == uuid.Nil {
		return fmt.Errorf("%w: goal_id is required", ErrValidation)
	}
	if !p.Status.Valid() {
		return fmt.Errorf("%w: unknown status %q", ErrValidation, p.Status)
	}
	for i, step := range p.Steps {
		if step.ID == "" {
			return fmt.Errorf("%w: step[%d] id is required", ErrValidation, i)
		}
		if step.Description == "" {
			return fmt.Errorf("%w: step[%d] description is required", ErrValidation, i)
		}
	}
	return nil
}

// Transition moves the plan into a new status.
func (p *Plan) Transition(target PlanStatus, now time.Time) error {
	if !p.Status.CanTransitionTo(target) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, p.Status, target)
	}
	p.Status = target
	p.UpdatedAt = now.UTC()
	return nil
}

// PlanStep is a node in the plan DAG. RequiredCapability lets the
// coordinator filter which agents can satisfy this step before
// looking at availability or load.
type PlanStep struct {
	ID                 string
	Order              int
	Description        string
	RequiredCapability *string
	DependsOn          []string
}
