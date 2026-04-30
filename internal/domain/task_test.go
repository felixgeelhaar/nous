package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTaskStatus_CanTransitionTo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		from, to TaskStatus
		want     bool
	}{
		{TaskPending, TaskAssigned, true},
		{TaskPending, TaskBlocked, true},
		{TaskPending, TaskRunning, false}, // must assign first
		{TaskAssigned, TaskRunning, true},
		{TaskAssigned, TaskPending, true}, // unassign
		{TaskRunning, TaskCompleted, true},
		{TaskRunning, TaskFailed, true},
		{TaskBlocked, TaskAssigned, true},
		{TaskCompleted, TaskRunning, false}, // terminal
		{TaskFailed, TaskRunning, false},
		{TaskCancelled, TaskPending, false},
	}
	for _, tc := range cases {
		if got := tc.from.CanTransitionTo(tc.to); got != tc.want {
			t.Errorf("%q -> %q: got %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestNewTask_Validates(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	t.Run("happy path", func(t *testing.T) {
		ts, err := NewTask("draft email", "send to alex", PriorityHigh, nil, now)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ts.Status != TaskPending {
			t.Errorf("status = %q, want pending", ts.Status)
		}
		if ts.Priority != PriorityHigh {
			t.Errorf("priority = %d, want %d", ts.Priority, PriorityHigh)
		}
	})

	t.Run("requires title", func(t *testing.T) {
		_, err := NewTask("", "desc", PriorityNormal, nil, now)
		if !errors.Is(err, ErrValidation) {
			t.Errorf("expected ErrValidation, got %v", err)
		}
	})
}

func TestTask_Transition(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	ts, err := NewTask("draft email", "send to alex", PriorityNormal, nil, now)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	later := now.Add(time.Hour)
	if err := ts.Transition(TaskAssigned, later); err != nil {
		t.Fatalf("pending->assigned: %v", err)
	}
	if ts.Status != TaskAssigned {
		t.Errorf("status = %q, want assigned", ts.Status)
	}
	if !ts.UpdatedAt.Equal(later) {
		t.Errorf("updated_at not refreshed: got %v, want %v", ts.UpdatedAt, later)
	}

	if err := ts.Transition(TaskCompleted, later); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition for assigned->completed, got %v", err)
	}
}

func TestAssignmentStatus_CanTransitionTo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		from, to AssignmentStatus
		want     bool
	}{
		{AssignmentAssigned, AssignmentRunning, true},
		{AssignmentAssigned, AssignmentCancelled, true},
		{AssignmentAssigned, AssignmentCompleted, false},
		{AssignmentRunning, AssignmentCompleted, true},
		{AssignmentRunning, AssignmentCancelled, true},
		{AssignmentCompleted, AssignmentRunning, false},
		{AssignmentCancelled, AssignmentAssigned, false},
		{AssignmentAssigned, AssignmentAssigned, true},
	}
	for _, tc := range cases {
		if got := tc.from.CanTransitionTo(tc.to); got != tc.want {
			t.Errorf("%q -> %q: got %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestNewAssignment_Validates(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	taskID := uuid.New()

	t.Run("happy path", func(t *testing.T) {
		a, err := NewAssignment(taskID, "agent-1", now)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.Status != AssignmentAssigned {
			t.Errorf("status = %q, want assigned", a.Status)
		}
		if a.TaskID != taskID {
			t.Errorf("task_id mismatch")
		}
	})

	t.Run("requires task_id", func(t *testing.T) {
		_, err := NewAssignment(uuid.Nil, "agent-1", now)
		if !errors.Is(err, ErrValidation) {
			t.Errorf("expected ErrValidation, got %v", err)
		}
	})

	t.Run("requires agent_id", func(t *testing.T) {
		_, err := NewAssignment(taskID, "", now)
		if !errors.Is(err, ErrValidation) {
			t.Errorf("expected ErrValidation, got %v", err)
		}
	})
}

func TestAssignment_Transition(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	a, err := NewAssignment(uuid.New(), "agent-1", now)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	later := now.Add(time.Hour)
	if err := a.Transition(AssignmentRunning, later); err != nil {
		t.Fatalf("assigned->running: %v", err)
	}
	if a.Status != AssignmentRunning {
		t.Errorf("status = %q, want running", a.Status)
	}
	if a.CompletedAt != nil {
		t.Error("completed_at should be nil for running")
	}

	if err := a.Transition(AssignmentCompleted, later.Add(time.Hour)); err != nil {
		t.Fatalf("running->completed: %v", err)
	}
	if a.Status != AssignmentCompleted {
		t.Errorf("status = %q, want completed", a.Status)
	}
	if a.CompletedAt == nil {
		t.Fatal("completed_at should be set for completed")
	}

	if err := a.Transition(AssignmentCancelled, later); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}
