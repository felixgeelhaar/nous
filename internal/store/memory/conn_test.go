package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/ports"
	"github.com/felixgeelhaar/nous/internal/store/memory"
)

func TestCommitmentRepo_SaveGetUpdateRisk(t *testing.T) {
	t.Parallel()
	conn := memory.New()
	ctx := context.Background()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	c, err := domain.NewCommitment("u1", "follow up with alex", nil, 0.9, now)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := conn.Commitments.Save(ctx, c); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := conn.Commitments.Get(ctx, c.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.OwnerID != "u1" || got.Description != "follow up with alex" {
		t.Errorf("got %+v", got)
	}

	later := now.Add(time.Hour)
	if err := conn.Commitments.UpdateRisk(ctx, c.ID, 0.42, later); err != nil {
		t.Fatalf("UpdateRisk: %v", err)
	}
	got2, _ := conn.Commitments.Get(ctx, c.ID)
	if got2.RiskScore != 0.42 {
		t.Errorf("risk_score = %v, want 0.42", got2.RiskScore)
	}
	if got2.LastEvaluatedAt == nil || !got2.LastEvaluatedAt.Equal(later) {
		t.Errorf("last_evaluated_at = %v, want %v", got2.LastEvaluatedAt, later)
	}
}

func TestCommitmentRepo_GetMissing(t *testing.T) {
	t.Parallel()
	conn := memory.New()
	_, err := conn.Commitments.Get(context.Background(), domain.CommitmentID([16]byte{}))
	if !errors.Is(err, domain.ErrCommitmentNotFound) {
		t.Errorf("expected ErrCommitmentNotFound, got %v", err)
	}
}

func TestCommitmentRepo_FindActiveOrdering(t *testing.T) {
	t.Parallel()
	conn := memory.New()
	ctx := context.Background()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	soon := now.Add(2 * time.Hour)
	later := now.Add(48 * time.Hour)

	highRiskSoon, _ := domain.NewCommitment("u1", "high risk", &soon, 0.9, now)
	highRiskSoon.RiskScore = 0.9
	mediumRisk, _ := domain.NewCommitment("u1", "medium risk", &later, 0.9, now.Add(time.Second))
	mediumRisk.RiskScore = 0.5
	completed, _ := domain.NewCommitment("u1", "done", nil, 0.9, now.Add(2*time.Second))
	if err := completed.Transition(domain.CommitmentCompleted, now.Add(3*time.Second)); err != nil {
		t.Fatalf("transition: %v", err)
	}

	for _, c := range []domain.Commitment{highRiskSoon, mediumRisk, completed} {
		if err := conn.Commitments.Save(ctx, c); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	active, err := conn.Commitments.FindActive(ctx, 0)
	if err != nil {
		t.Fatalf("find active: %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("expected 2 active, got %d", len(active))
	}
	if active[0].RiskScore < active[1].RiskScore {
		t.Errorf("expected high-risk first; got %v then %v", active[0].RiskScore, active[1].RiskScore)
	}
}

func TestCommitmentRepo_DefensiveCopy(t *testing.T) {
	t.Parallel()
	conn := memory.New()
	ctx := context.Background()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	c, _ := domain.NewCommitment("u1", "task", nil, 0.9, now)
	c.SourceRefs = []domain.SourceRef{{Kind: domain.SourceMnemosEvent, Locator: "evt-1"}}
	if err := conn.Commitments.Save(ctx, c); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Mutate the local copy after save; the store must not see it.
	c.SourceRefs[0].Locator = "tampered"

	stored, _ := conn.Commitments.Get(ctx, c.ID)
	if stored.SourceRefs[0].Locator == "tampered" {
		t.Error("memory store leaked aliased slice on Save")
	}

	// Now mutate the returned struct; subsequent reads must not see it.
	stored.SourceRefs[0].Locator = "tampered2"
	stored2, _ := conn.Commitments.Get(ctx, c.ID)
	if stored2.SourceRefs[0].Locator == "tampered2" {
		t.Error("memory store leaked aliased slice on Get")
	}
}

func TestInterventionRepo_RoundTrip(t *testing.T) {
	t.Parallel()
	conn := memory.New()
	ctx := context.Background()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	c, _ := domain.NewCommitment("u1", "task", nil, 0.9, now)
	if err := conn.Commitments.Save(ctx, c); err != nil {
		t.Fatalf("save commitment: %v", err)
	}

	iv, err := domain.NewIntervention(domain.InterventionNudge, "ping alex", &c.ID, nil, nil, now)
	if err != nil {
		t.Fatalf("intervention: %v", err)
	}
	if err := conn.Interventions.Save(ctx, iv); err != nil {
		t.Fatalf("save iv: %v", err)
	}

	list, err := conn.Interventions.List(ctx, ports.InterventionFilter{
		CommitmentID: &c.ID,
		Statuses:     []domain.InterventionStatus{domain.InterventionPending},
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != iv.ID {
		t.Errorf("unexpected list: %+v", list)
	}
}

func TestDecisionRepo_ListBySubject(t *testing.T) {
	t.Parallel()
	conn := memory.New()
	ctx := context.Background()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	for i, subject := range []string{"intervene", "intervene", "skip"} {
		d, err := domain.NewDecision(subject, "test reason", 0.5, now.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatalf("new: %v", err)
		}
		if err := conn.Decisions.Save(ctx, d); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	got, err := conn.Decisions.ListBySubject(ctx, "intervene", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
	if got[0].CreatedAt.Before(got[1].CreatedAt) {
		t.Error("expected most-recent-first ordering")
	}
}

func TestDecisionRepo_GetMissing(t *testing.T) {
	t.Parallel()
	conn := memory.New()
	_, err := conn.Decisions.Get(context.Background(), domain.DecisionID([16]byte{}))
	if !errors.Is(err, domain.ErrDecisionNotFound) {
		t.Errorf("expected ErrDecisionNotFound, got %v", err)
	}
}

func TestGoalRepo_SaveGetListByOwner(t *testing.T) {
	t.Parallel()
	conn := memory.New()
	ctx := context.Background()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	g, err := domain.NewGoal("u1", "Ship feature", "description", domain.PriorityHigh, now)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := conn.Goals.Save(ctx, g); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := conn.Goals.Get(ctx, g.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "Ship feature" {
		t.Errorf("title = %q, want Ship feature", got.Title)
	}

	list, err := conn.Goals.ListByOwner(ctx, "u1", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != g.ID {
		t.Errorf("unexpected list: %+v", list)
	}

	_, err = conn.Goals.Get(ctx, domain.GoalID([16]byte{}))
	if !errors.Is(err, domain.ErrGoalNotFound) {
		t.Errorf("expected ErrGoalNotFound, got %v", err)
	}
}

func TestTaskRepo_SaveGetListFilter(t *testing.T) {
	t.Parallel()
	conn := memory.New()
	ctx := context.Background()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	ts, err := domain.NewTask("draft email", "send to alex", domain.PriorityNormal, nil, now)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := conn.Tasks.Save(ctx, ts); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := conn.Tasks.Get(ctx, ts.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "draft email" {
		t.Errorf("title = %q, want draft email", got.Title)
	}

	list, err := conn.Tasks.List(ctx, ports.TaskFilter{Statuses: []domain.TaskStatus{domain.TaskPending}})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != ts.ID {
		t.Errorf("unexpected list: %+v", list)
	}

	_, err = conn.Tasks.Get(ctx, domain.TaskID([16]byte{}))
	if !errors.Is(err, domain.ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestInterventionRepo_GetMissing(t *testing.T) {
	t.Parallel()
	conn := memory.New()
	_, err := conn.Interventions.Get(context.Background(), domain.InterventionID([16]byte{}))
	if !errors.Is(err, domain.ErrInterventionNotFound) {
		t.Errorf("expected ErrInterventionNotFound, got %v", err)
	}
}

func TestCommitmentRepo_ListFilter(t *testing.T) {
	t.Parallel()
	conn := memory.New()
	ctx := context.Background()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	due := now.Add(24 * time.Hour)

	c1, _ := domain.NewCommitment("u1", "task 1", &due, 0.9, now)
	c2, _ := domain.NewCommitment("u2", "task 2", nil, 0.8, now.Add(time.Second))
	c3, _ := domain.NewCommitment("u1", "task 3", nil, 0.7, now.Add(2*time.Second))
	if err := c3.Transition(domain.CommitmentInProgress, now); err != nil {
		t.Fatalf("transition: %v", err)
	}

	for _, c := range []domain.Commitment{c1, c2, c3} {
		if err := conn.Commitments.Save(ctx, c); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	byOwner, err := conn.Commitments.List(ctx, ports.CommitmentFilter{OwnerID: "u1"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(byOwner) != 2 {
		t.Fatalf("expected 2 by owner u1, got %d", len(byOwner))
	}

	byStatus, err := conn.Commitments.List(ctx, ports.CommitmentFilter{Statuses: []domain.CommitmentStatus{domain.CommitmentInProgress}})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(byStatus) != 1 || byStatus[0].ID != c3.ID {
		t.Fatalf("expected 1 in_progress, got %+v", byStatus)
	}

	byDue, err := conn.Commitments.List(ctx, ports.CommitmentFilter{DueBy: &due})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(byDue) != 1 || byDue[0].ID != c1.ID {
		t.Fatalf("expected 1 due by %v, got %+v", due, byDue)
	}
}
