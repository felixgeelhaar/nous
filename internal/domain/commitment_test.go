package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCommitmentStatus_IsActive(t *testing.T) {
	t.Parallel()
	cases := []struct {
		s    CommitmentStatus
		want bool
	}{
		{CommitmentPending, true},
		{CommitmentInProgress, true},
		{CommitmentCompleted, false},
		{CommitmentMissed, false},
		{CommitmentCancelled, false},
		{CommitmentStatus("garbage"), false},
	}
	for _, tc := range cases {
		if got := tc.s.IsActive(); got != tc.want {
			t.Errorf("IsActive(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

func TestCommitmentStatus_CanTransitionTo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		from, to CommitmentStatus
		want     bool
	}{
		{CommitmentPending, CommitmentInProgress, true},
		{CommitmentPending, CommitmentCompleted, true},
		{CommitmentPending, CommitmentMissed, true},
		{CommitmentPending, CommitmentCancelled, true},
		{CommitmentInProgress, CommitmentCompleted, true},
		{CommitmentInProgress, CommitmentMissed, true},
		{CommitmentInProgress, CommitmentPending, false},
		{CommitmentCompleted, CommitmentInProgress, false},
		{CommitmentCancelled, CommitmentPending, false},
		{CommitmentMissed, CommitmentCompleted, false},
		{CommitmentCompleted, CommitmentCompleted, true}, // self is no-op
	}
	for _, tc := range cases {
		if got := tc.from.CanTransitionTo(tc.to); got != tc.want {
			t.Errorf("%q -> %q: got %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestNewCommitment_Validates(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	due := now.Add(24 * time.Hour)

	cases := []struct {
		name        string
		ownerID     string
		description string
		due         *time.Time
		confidence  float64
		wantErr     bool
	}{
		{"happy path", "u1", "follow up with Alex", &due, 0.9, false},
		{"no owner", "", "follow up", &due, 0.9, true},
		{"no description", "u1", "", &due, 0.9, true},
		{"low confidence ok", "u1", "ping team", nil, 0.0, false},
		{"high confidence ok", "u1", "ping team", nil, 1.0, false},
		{"negative confidence", "u1", "ping team", nil, -0.1, true},
		{"over-one confidence", "u1", "ping team", nil, 1.5, true},
		{"trims whitespace", "  u1  ", "  ping team  ", nil, 0.9, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewCommitment(tc.ownerID, tc.description, tc.due, tc.confidence, now)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", c)
				}
				if !errors.Is(err, ErrValidation) {
					t.Errorf("err = %v, want wrapping ErrValidation", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c.Status != CommitmentPending {
				t.Errorf("status = %q, want pending", c.Status)
			}
			if c.OwnerID != "u1" {
				t.Errorf("owner_id = %q, want trimmed u1", c.OwnerID)
			}
			if !c.CreatedAt.Equal(now) {
				t.Errorf("created_at = %v, want %v", c.CreatedAt, now)
			}
		})
	}
}

func TestCommitment_Transition(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	c, err := NewCommitment("u1", "task", nil, 0.8, now)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	later := now.Add(time.Hour)
	if err := c.Transition(CommitmentInProgress, later); err != nil {
		t.Fatalf("pending->in_progress: %v", err)
	}
	if c.Status != CommitmentInProgress {
		t.Errorf("status = %q, want in_progress", c.Status)
	}
	if !c.UpdatedAt.Equal(later) {
		t.Errorf("updated_at not refreshed: got %v, want %v", c.UpdatedAt, later)
	}

	// Illegal: cannot move from in_progress back to pending.
	if err := c.Transition(CommitmentPending, later); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestCommitment_Validate(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		c    Commitment
		want bool
	}{
		{"valid", Commitment{ID: uuid.New(), OwnerID: "u1", Description: "task", Status: CommitmentPending, Confidence: 0.5, CreatedAt: now, UpdatedAt: now}, false},
		{"missing id", Commitment{OwnerID: "u1", Description: "task", Status: CommitmentPending, Confidence: 0.5, CreatedAt: now, UpdatedAt: now}, true},
		{"missing owner", Commitment{ID: uuid.New(), Description: "task", Status: CommitmentPending, Confidence: 0.5, CreatedAt: now, UpdatedAt: now}, true},
		{"bad confidence", Commitment{ID: uuid.New(), OwnerID: "u1", Description: "task", Status: CommitmentPending, Confidence: 1.5, CreatedAt: now, UpdatedAt: now}, true},
		{"bad status", Commitment{ID: uuid.New(), OwnerID: "u1", Description: "task", Status: CommitmentStatus("garbage"), Confidence: 0.5, CreatedAt: now, UpdatedAt: now}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.c.Validate()
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

func TestCommitmentDraft_ToCommitment(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	due := now.Add(48 * time.Hour)

	d := CommitmentDraft{
		OwnerID:     "u1",
		Description: "ship report",
		DueAt:       &due,
		Confidence:  0.82,
		Entities:    []EntityRef{{Kind: EntityProject, Name: "Q2 review"}},
		SourceRefs:  []SourceRef{{Kind: SourceMnemosEvent, Locator: "evt-123"}},
	}
	c, err := d.ToCommitment(now)
	if err != nil {
		t.Fatalf("ToCommitment: %v", err)
	}
	if c.Confidence != 0.82 {
		t.Errorf("confidence = %v, want 0.82", c.Confidence)
	}
	if len(c.Entities) != 1 || c.Entities[0].Name != "Q2 review" {
		t.Errorf("entities not copied: %+v", c.Entities)
	}
	if len(c.SourceRefs) != 1 || c.SourceRefs[0].Locator != "evt-123" {
		t.Errorf("source refs not copied: %+v", c.SourceRefs)
	}

	// Mutate the draft after the fact; the commitment must not see it
	// (defensive copy).
	d.Entities[0].Name = "mutated"
	if c.Entities[0].Name == "mutated" {
		t.Error("ToCommitment did not defensively copy entities")
	}
}
