package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPlanStatus_Valid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		s    PlanStatus
		want bool
	}{
		{PlanDraft, true},
		{PlanActive, true},
		{PlanCompleted, true},
		{PlanSuperseded, true},
		{PlanStatus("garbage"), false},
	}
	for _, tc := range cases {
		if got := tc.s.Valid(); got != tc.want {
			t.Errorf("Valid(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

func TestPlanStatus_CanTransitionTo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		from, to PlanStatus
		want     bool
	}{
		{PlanDraft, PlanActive, true},
		{PlanDraft, PlanSuperseded, true},
		{PlanDraft, PlanCompleted, false},
		{PlanActive, PlanCompleted, true},
		{PlanActive, PlanSuperseded, true},
		{PlanCompleted, PlanActive, false},
		{PlanSuperseded, PlanActive, false},
		{PlanDraft, PlanDraft, true},
	}
	for _, tc := range cases {
		if got := tc.from.CanTransitionTo(tc.to); got != tc.want {
			t.Errorf("%q -> %q: got %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestPlan_Validate(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		p    Plan
		want bool
	}{
		{
			"valid",
			Plan{ID: uuid.New(), GoalID: uuid.New(), Status: PlanDraft, CreatedAt: now, UpdatedAt: now},
			false,
		},
		{
			"missing id",
			Plan{GoalID: uuid.New(), Status: PlanDraft, CreatedAt: now, UpdatedAt: now},
			true,
		},
		{
			"missing goal_id",
			Plan{ID: uuid.New(), Status: PlanDraft, CreatedAt: now, UpdatedAt: now},
			true,
		},
		{
			"bad status",
			Plan{ID: uuid.New(), GoalID: uuid.New(), Status: PlanStatus("garbage"), CreatedAt: now, UpdatedAt: now},
			true,
		},
		{
			"step missing id",
			Plan{ID: uuid.New(), GoalID: uuid.New(), Status: PlanDraft, Steps: []PlanStep{{Description: "step"}}, CreatedAt: now, UpdatedAt: now},
			true,
		},
		{
			"step missing description",
			Plan{ID: uuid.New(), GoalID: uuid.New(), Status: PlanDraft, Steps: []PlanStep{{ID: "s1"}}, CreatedAt: now, UpdatedAt: now},
			true,
		},
		{
			"valid with steps",
			Plan{ID: uuid.New(), GoalID: uuid.New(), Status: PlanDraft, Steps: []PlanStep{{ID: "s1", Description: "do thing"}}, CreatedAt: now, UpdatedAt: now},
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.p.Validate()
			if tc.want {
				if !errors.Is(err, ErrValidation) {
					t.Errorf("expected ErrValidation, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestPlan_Transition(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	p := Plan{
		ID:        uuid.New(),
		GoalID:    uuid.New(),
		Status:    PlanDraft,
		CreatedAt: now,
		UpdatedAt: now,
	}

	later := now.Add(time.Hour)
	if err := p.Transition(PlanActive, later); err != nil {
		t.Fatalf("draft->active: %v", err)
	}
	if p.Status != PlanActive {
		t.Errorf("status = %q, want active", p.Status)
	}
	if !p.UpdatedAt.Equal(later) {
		t.Errorf("updated_at not refreshed: got %v, want %v", p.UpdatedAt, later)
	}

	if err := p.Transition(PlanDraft, later); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}
