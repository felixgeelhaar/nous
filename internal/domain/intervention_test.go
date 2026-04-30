package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestInterventionType_Valid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		t    InterventionType
		want bool
	}{
		{InterventionNudge, true},
		{InterventionSuggestion, true},
		{InterventionEscalation, true},
		{InterventionAutomation, true},
		{InterventionType("garbage"), false},
	}
	for _, tc := range cases {
		if got := tc.t.Valid(); got != tc.want {
			t.Errorf("Valid(%q) = %v, want %v", tc.t, got, tc.want)
		}
	}
}

func TestInterventionStatus_CanTransitionTo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		from, to InterventionStatus
		want     bool
	}{
		{InterventionPending, InterventionAccepted, true},
		{InterventionPending, InterventionRejected, true},
		{InterventionPending, InterventionExpired, true},
		{InterventionPending, InterventionExecuted, false}, // must accept first
		{InterventionAccepted, InterventionExecuted, true},
		{InterventionAccepted, InterventionRejected, false}, // sticky once accepted
		{InterventionRejected, InterventionAccepted, false},
		{InterventionExpired, InterventionAccepted, false},
		{InterventionExecuted, InterventionRejected, false},
		{InterventionPending, InterventionPending, true}, // self is no-op
	}
	for _, tc := range cases {
		if got := tc.from.CanTransitionTo(tc.to); got != tc.want {
			t.Errorf("%q -> %q: got %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestNewIntervention_Validates(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	commitmentID := uuid.New()

	t.Run("requires anchor", func(t *testing.T) {
		_, err := NewIntervention(InterventionNudge, "ping", nil, nil, nil, now)
		if !errors.Is(err, ErrValidation) {
			t.Errorf("expected ErrValidation, got %v", err)
		}
	})

	t.Run("rejects unknown type", func(t *testing.T) {
		_, err := NewIntervention(InterventionType("garbage"), "msg", &commitmentID, nil, nil, now)
		if !errors.Is(err, ErrValidation) {
			t.Errorf("expected ErrValidation, got %v", err)
		}
	})

	t.Run("rejects empty message", func(t *testing.T) {
		_, err := NewIntervention(InterventionNudge, "  ", &commitmentID, nil, nil, now)
		if !errors.Is(err, ErrValidation) {
			t.Errorf("expected ErrValidation, got %v", err)
		}
	})

	t.Run("happy path", func(t *testing.T) {
		iv, err := NewIntervention(InterventionNudge, "follow up due", &commitmentID, nil, nil, now)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if iv.Status != InterventionPending {
			t.Errorf("status = %q, want pending", iv.Status)
		}
		if !iv.TriggeredAt.Equal(now) {
			t.Errorf("triggered_at = %v, want %v", iv.TriggeredAt, now)
		}
	})
}

func TestIntervention_Resolve(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	commitmentID := uuid.New()
	iv, err := NewIntervention(InterventionSuggestion, "send reminder", &commitmentID, nil, nil, now)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	later := now.Add(5 * time.Minute)
	if err := iv.Resolve(InterventionAccepted, later); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if iv.ResolvedAt == nil || !iv.ResolvedAt.Equal(later) {
		t.Errorf("resolved_at = %v, want %v", iv.ResolvedAt, later)
	}

	if err := iv.Resolve(InterventionExecuted, later.Add(time.Minute)); err != nil {
		t.Fatalf("accept->executed: %v", err)
	}

	// Cannot regress.
	if err := iv.Resolve(InterventionRejected, later.Add(2*time.Minute)); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}
