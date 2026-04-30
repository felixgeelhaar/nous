package domain

import (
	"errors"
	"testing"
	"time"
)

func TestGoalStatus_CanTransitionTo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		from, to GoalStatus
		want     bool
	}{
		{GoalOpen, GoalPlanned, true},
		{GoalOpen, GoalActive, false},
		{GoalOpen, GoalCancelled, true},
		{GoalPlanned, GoalActive, true},
		{GoalPlanned, GoalCompleted, false},
		{GoalPlanned, GoalCancelled, true},
		{GoalActive, GoalCompleted, true},
		{GoalActive, GoalCancelled, true},
		{GoalCompleted, GoalActive, false},
		{GoalCancelled, GoalOpen, false},
		{GoalOpen, GoalOpen, true},
	}
	for _, tc := range cases {
		if got := tc.from.CanTransitionTo(tc.to); got != tc.want {
			t.Errorf("%q -> %q: got %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestNewGoal_Validates(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	t.Run("happy path", func(t *testing.T) {
		g, err := NewGoal("u1", "Ship Q2 review", "summary doc + meeting", PriorityHigh, now)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if g.Status != GoalOpen {
			t.Errorf("status = %q, want open", g.Status)
		}
	})

	t.Run("requires title", func(t *testing.T) {
		_, err := NewGoal("u1", "", "desc", PriorityNormal, now)
		if !errors.Is(err, ErrValidation) {
			t.Errorf("expected ErrValidation, got %v", err)
		}
	})

	t.Run("requires owner", func(t *testing.T) {
		_, err := NewGoal("", "title", "desc", PriorityNormal, now)
		if !errors.Is(err, ErrValidation) {
			t.Errorf("expected ErrValidation, got %v", err)
		}
	})
}

func TestGoal_Transition(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	g, err := NewGoal("u1", "Ship Q2 review", "desc", PriorityNormal, now)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	later := now.Add(time.Hour)
	if err := g.Transition(GoalPlanned, later); err != nil {
		t.Fatalf("open->planned: %v", err)
	}
	if g.Status != GoalPlanned {
		t.Errorf("status = %q, want planned", g.Status)
	}
	if !g.UpdatedAt.Equal(later) {
		t.Errorf("updated_at not refreshed: got %v, want %v", g.UpdatedAt, later)
	}

	if err := g.Transition(GoalOpen, later); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}
