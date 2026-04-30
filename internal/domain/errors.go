package domain

import "errors"

// Sentinel errors. Repositories return these so that callers can
// branch on identity without inspecting strings; outer layers wrap
// them with %w when they need additional context.
var (
	ErrCommitmentNotFound   = errors.New("nous: commitment not found")
	ErrDecisionNotFound     = errors.New("nous: decision not found")
	ErrInterventionNotFound = errors.New("nous: intervention not found")
	ErrGoalNotFound         = errors.New("nous: goal not found")
	ErrTaskNotFound         = errors.New("nous: task not found")

	// ErrInvalidTransition is returned when a status update would
	// move a record into a state its lifecycle does not permit
	// (e.g. completing a cancelled commitment). Aggregates own
	// their own transition tables; this error is the uniform
	// signal at the boundary.
	ErrInvalidTransition = errors.New("nous: invalid status transition")

	// ErrValidation is returned when a struct fails its own
	// Validate() check before persistence. Aggregates wrap this
	// with the offending field name.
	ErrValidation = errors.New("nous: validation failed")
)
