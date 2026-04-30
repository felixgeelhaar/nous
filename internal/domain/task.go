package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// TaskStatus is the lifecycle of a unit of executable work. Pending
// means the task has been created but not assigned; assigned has an
// AgentID set but execution has not begun; running is in flight;
// blocked is waiting on a dependency; completed/failed/cancelled are
// terminal.
type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskAssigned  TaskStatus = "assigned"
	TaskRunning   TaskStatus = "running"
	TaskBlocked   TaskStatus = "blocked"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskCancelled TaskStatus = "cancelled"
)

// Valid reports whether s is a known status.
func (s TaskStatus) Valid() bool {
	switch s {
	case TaskPending, TaskAssigned, TaskRunning, TaskBlocked,
		TaskCompleted, TaskFailed, TaskCancelled:
		return true
	}
	return false
}

// IsTerminal reports whether the status is a final state.
func (s TaskStatus) IsTerminal() bool {
	switch s {
	case TaskCompleted, TaskFailed, TaskCancelled:
		return true
	}
	return false
}

// CanTransitionTo enumerates legal status edges.
func (s TaskStatus) CanTransitionTo(target TaskStatus) bool {
	if s == target {
		return true
	}
	if s.IsTerminal() {
		return false
	}
	switch s {
	case TaskPending:
		return target == TaskAssigned || target == TaskBlocked || target == TaskCancelled
	case TaskAssigned:
		return target == TaskRunning || target == TaskBlocked ||
			target == TaskCancelled || target == TaskPending
	case TaskRunning:
		return target == TaskCompleted || target == TaskFailed ||
			target == TaskBlocked || target == TaskCancelled
	case TaskBlocked:
		return target == TaskPending || target == TaskAssigned || target == TaskCancelled
	}
	return false
}

// Task is a concrete unit of work, optionally tied back to a goal,
// plan, or commitment. AssignedTo points at the agent (or human)
// expected to execute; nil means unassigned.
type Task struct {
	ID           TaskID
	GoalID       *GoalID
	PlanID       *PlanID
	CommitmentID *CommitmentID
	Title        string
	Description  string
	AssignedTo   *AgentID
	Status       TaskStatus
	Priority     Priority
	DueAt        *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NewTask constructs a fresh pending task.
func NewTask(
	title string,
	description string,
	priority Priority,
	dueAt *time.Time,
	now time.Time,
) (Task, error) {
	t := Task{
		ID:          uuid.New(),
		Title:       strings.TrimSpace(title),
		Description: strings.TrimSpace(description),
		Priority:    priority,
		Status:      TaskPending,
		DueAt:       dueAt,
		CreatedAt:   now.UTC(),
		UpdatedAt:   now.UTC(),
	}
	if err := t.Validate(); err != nil {
		return Task{}, err
	}
	return t, nil
}

// Validate enforces the minimum invariants.
func (t Task) Validate() error {
	if t.ID == uuid.Nil {
		return fmt.Errorf("%w: id is required", ErrValidation)
	}
	if t.Title == "" {
		return fmt.Errorf("%w: title is required", ErrValidation)
	}
	if !t.Status.Valid() {
		return fmt.Errorf("%w: unknown status %q", ErrValidation, t.Status)
	}
	return nil
}

// Transition moves the task into a new status. AssignedTo is not
// touched here; the caller is expected to set or clear it as part of
// the same update.
func (t *Task) Transition(target TaskStatus, now time.Time) error {
	if !t.Status.CanTransitionTo(target) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, t.Status, target)
	}
	t.Status = target
	t.UpdatedAt = now.UTC()
	return nil
}

// AssignmentStatus tracks the lifecycle of an agent picking up a
// task. Assigned is the initial state; running mirrors the task
// state machine; completed/cancelled are terminal.
type AssignmentStatus string

const (
	AssignmentAssigned  AssignmentStatus = "assigned"
	AssignmentRunning   AssignmentStatus = "running"
	AssignmentCompleted AssignmentStatus = "completed"
	AssignmentCancelled AssignmentStatus = "cancelled"
)

// NewAssignment constructs a fresh assigned assignment.
func NewAssignment(taskID TaskID, agentID AgentID, now time.Time) (Assignment, error) {
	a := Assignment{
		ID:         uuid.New(),
		TaskID:     taskID,
		AgentID:    agentID,
		Status:     AssignmentAssigned,
		AssignedAt: now.UTC(),
	}
	if err := a.Validate(); err != nil {
		return Assignment{}, err
	}
	return a, nil
}

// Validate enforces the minimum invariants.
func (a Assignment) Validate() error {
	if a.ID == uuid.Nil {
		return fmt.Errorf("%w: id is required", ErrValidation)
	}
	if a.TaskID == uuid.Nil {
		return fmt.Errorf("%w: task_id is required", ErrValidation)
	}
	if a.AgentID == "" {
		return fmt.Errorf("%w: agent_id is required", ErrValidation)
	}
	if !a.Status.Valid() {
		return fmt.Errorf("%w: unknown status %q", ErrValidation, a.Status)
	}
	return nil
}

// CanTransitionTo enumerates legal status edges.
func (s AssignmentStatus) CanTransitionTo(target AssignmentStatus) bool {
	if s == target {
		return true
	}
	switch s {
	case AssignmentAssigned:
		return target == AssignmentRunning || target == AssignmentCancelled
	case AssignmentRunning:
		return target == AssignmentCompleted || target == AssignmentCancelled
	default:
		return false
	}
}

// Valid reports whether s is a known assignment status.
func (s AssignmentStatus) Valid() bool {
	switch s {
	case AssignmentAssigned, AssignmentRunning, AssignmentCompleted, AssignmentCancelled:
		return true
	}
	return false
}

// Transition moves the assignment into a new status.
func (a *Assignment) Transition(target AssignmentStatus, now time.Time) error {
	if !a.Status.CanTransitionTo(target) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, a.Status, target)
	}
	a.Status = target
	if target == AssignmentCompleted || target == AssignmentCancelled {
		t := now.UTC()
		a.CompletedAt = &t
	}
	return nil
}

// Assignment ties a Task to an AgentID with its own lifecycle. One
// task can have multiple historical assignments (re-assignment
// after a failure leaves an audit trail).
type Assignment struct {
	ID          AssignmentID
	TaskID      TaskID
	AgentID     AgentID
	Status      AssignmentStatus
	AssignedAt  time.Time
	CompletedAt *time.Time
}
