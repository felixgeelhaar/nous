package sqlite_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/ports"
	"github.com/felixgeelhaar/nous/internal/store/sqlite"
)

// openTestConn returns a fresh in-memory database backed by a unique
// shared cache so each test gets isolation while reusing the driver.
func openTestConn(t *testing.T) *sqlite.Conn {
	t.Helper()
	c, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestSQLite_CommitmentRoundTrip(t *testing.T) {
	t.Parallel()
	conn := openTestConn(t)
	ctx := context.Background()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	due := now.Add(2 * time.Hour)

	c, err := domain.NewCommitment("u1", "follow up with alex", &due, 0.85, now)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	c.SourceRefs = []domain.SourceRef{{Kind: domain.SourceMnemosEvent, Locator: "evt-1"}}
	c.Entities = []domain.EntityRef{{Kind: domain.EntityPerson, Name: "Alex"}}

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
	if len(got.SourceRefs) != 1 || got.SourceRefs[0].Locator != "evt-1" {
		t.Errorf("source_refs round-trip failed: %+v", got.SourceRefs)
	}
	if len(got.Entities) != 1 || got.Entities[0].Name != "Alex" {
		t.Errorf("entities round-trip failed: %+v", got.Entities)
	}
	if got.DueAt == nil || !got.DueAt.Equal(due) {
		t.Errorf("due_at round-trip: got %v want %v", got.DueAt, due)
	}
}

func TestSQLite_GetMissing(t *testing.T) {
	t.Parallel()
	conn := openTestConn(t)
	ctx := context.Background()

	_, err := conn.Commitments.Get(ctx, domain.CommitmentID([16]byte{}))
	if !errors.Is(err, domain.ErrCommitmentNotFound) {
		t.Errorf("commitment: expected ErrCommitmentNotFound, got %v", err)
	}
	_, err = conn.Decisions.Get(ctx, domain.DecisionID([16]byte{}))
	if !errors.Is(err, domain.ErrDecisionNotFound) {
		t.Errorf("decision: expected ErrDecisionNotFound, got %v", err)
	}
	_, err = conn.Interventions.Get(ctx, domain.InterventionID([16]byte{}))
	if !errors.Is(err, domain.ErrInterventionNotFound) {
		t.Errorf("intervention: expected ErrInterventionNotFound, got %v", err)
	}
	_, err = conn.Goals.Get(ctx, domain.GoalID([16]byte{}))
	if !errors.Is(err, domain.ErrGoalNotFound) {
		t.Errorf("goal: expected ErrGoalNotFound, got %v", err)
	}
	_, err = conn.Tasks.Get(ctx, domain.TaskID([16]byte{}))
	if !errors.Is(err, domain.ErrTaskNotFound) {
		t.Errorf("task: expected ErrTaskNotFound, got %v", err)
	}
}

func TestSQLite_FindActiveOrdering(t *testing.T) {
	t.Parallel()
	conn := openTestConn(t)
	ctx := context.Background()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	soon := now.Add(2 * time.Hour)
	later := now.Add(48 * time.Hour)

	high, _ := domain.NewCommitment("u1", "high", &soon, 0.9, now)
	high.RiskScore = 0.9
	medium, _ := domain.NewCommitment("u1", "medium", &later, 0.9, now.Add(time.Second))
	medium.RiskScore = 0.4
	done, _ := domain.NewCommitment("u1", "done", nil, 0.9, now.Add(2*time.Second))
	if err := done.Transition(domain.CommitmentCompleted, now.Add(3*time.Second)); err != nil {
		t.Fatalf("transition: %v", err)
	}

	for _, c := range []domain.Commitment{high, medium, done} {
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
		t.Errorf("ordering wrong: %+v", active)
	}
}

func TestSQLite_UpdateRisk(t *testing.T) {
	t.Parallel()
	conn := openTestConn(t)
	ctx := context.Background()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	c, _ := domain.NewCommitment("u1", "task", nil, 0.7, now)
	if err := conn.Commitments.Save(ctx, c); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := conn.Commitments.UpdateRisk(ctx, c.ID, 0.55, now.Add(time.Hour)); err != nil {
		t.Fatalf("update risk: %v", err)
	}
	got, _ := conn.Commitments.Get(ctx, c.ID)
	if got.RiskScore != 0.55 {
		t.Errorf("risk_score = %v, want 0.55", got.RiskScore)
	}
	if got.LastEvaluatedAt == nil {
		t.Error("expected last_evaluated_at to be set")
	}
}

func TestSQLite_InterventionWithSuggestedAction(t *testing.T) {
	t.Parallel()
	conn := openTestConn(t)
	ctx := context.Background()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	c, _ := domain.NewCommitment("u1", "task", nil, 0.9, now)
	if err := conn.Commitments.Save(ctx, c); err != nil {
		t.Fatalf("save commitment: %v", err)
	}

	action := &domain.ActionRequest{
		Capability:     "send_message",
		Payload:        map[string]any{"channel": "slack", "to": "alex"},
		IdempotencyKey: "iv-1",
	}
	iv, err := domain.NewIntervention(domain.InterventionSuggestion, "ping alex", &c.ID, nil, action, now)
	if err != nil {
		t.Fatalf("intervention: %v", err)
	}
	if err := conn.Interventions.Save(ctx, iv); err != nil {
		t.Fatalf("save iv: %v", err)
	}

	got, err := conn.Interventions.Get(ctx, iv.ID)
	if err != nil {
		t.Fatalf("get iv: %v", err)
	}
	if got.SuggestedAction == nil {
		t.Fatal("expected suggested_action to round-trip")
	}
	if got.SuggestedAction.Capability != "send_message" {
		t.Errorf("capability = %q, want send_message", got.SuggestedAction.Capability)
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

func TestSQLite_Decisions(t *testing.T) {
	t.Parallel()
	conn := openTestConn(t)
	ctx := context.Background()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 3; i++ {
		d, err := domain.NewDecision("intervene", "test reason", 0.5, now.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		d.Inputs = domain.DecisionInputs{"i": i}
		d.Outcome = domain.DecisionOutcome{"action": "nudge"}
		if err := conn.Decisions.Save(ctx, d); err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	list, err := conn.Decisions.ListBySubject(ctx, "intervene", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3, got %d", len(list))
	}
	for i := 1; i < len(list); i++ {
		if list[i-1].CreatedAt.Before(list[i].CreatedAt) {
			t.Errorf("ordering wrong at %d", i)
		}
	}
	if _, ok := list[0].Inputs["i"]; !ok {
		t.Errorf("inputs round-trip failed: %+v", list[0].Inputs)
	}
}

func TestSQLite_GoalAndTask(t *testing.T) {
	t.Parallel()
	conn := openTestConn(t)
	ctx := context.Background()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	g, err := domain.NewGoal("u1", "Q2 review", "ship summary doc", domain.PriorityHigh, now)
	if err != nil {
		t.Fatalf("goal: %v", err)
	}
	if err := conn.Goals.Save(ctx, g); err != nil {
		t.Fatalf("save goal: %v", err)
	}

	tk, err := domain.NewTask("draft section", "intro", domain.PriorityNormal, nil, now)
	if err != nil {
		t.Fatalf("task: %v", err)
	}
	gid := g.ID
	tk.GoalID = &gid
	if err := conn.Tasks.Save(ctx, tk); err != nil {
		t.Fatalf("save task: %v", err)
	}

	tasks, err := conn.Tasks.List(ctx, ports.TaskFilter{GoalID: &gid})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != tk.ID {
		t.Errorf("unexpected tasks: %+v", tasks)
	}

	goals, err := conn.Goals.ListByOwner(ctx, "u1", 0)
	if err != nil {
		t.Fatalf("list goals: %v", err)
	}
	if len(goals) != 1 || goals[0].ID != g.ID {
		t.Errorf("unexpected goals: %+v", goals)
	}
}

func TestSQLite_UpdateRiskMissing(t *testing.T) {
	t.Parallel()
	conn := openTestConn(t)
	ctx := context.Background()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	err := conn.Commitments.UpdateRisk(ctx, domain.CommitmentID([16]byte{}), 0.5, now)
	if !errors.Is(err, domain.ErrCommitmentNotFound) {
		t.Errorf("expected ErrCommitmentNotFound, got %v", err)
	}
}

func TestSQLite_CommitmentListFilter(t *testing.T) {
	t.Parallel()
	conn := openTestConn(t)
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
}

func TestSQLite_TaskListFilter(t *testing.T) {
	t.Parallel()
	conn := openTestConn(t)
	ctx := context.Background()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	tk1, _ := domain.NewTask("task 1", "desc 1", domain.PriorityNormal, nil, now)
	tk2, _ := domain.NewTask("task 2", "desc 2", domain.PriorityHigh, nil, now.Add(time.Second))
	agent := domain.AgentID("agent-1")
	tk2.AssignedTo = &agent
	if err := tk2.Transition(domain.TaskAssigned, now); err != nil {
		t.Fatalf("transition: %v", err)
	}

	for _, tk := range []domain.Task{tk1, tk2} {
		if err := conn.Tasks.Save(ctx, tk); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	byStatus, err := conn.Tasks.List(ctx, ports.TaskFilter{Statuses: []domain.TaskStatus{domain.TaskAssigned}})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(byStatus) != 1 || byStatus[0].ID != tk2.ID {
		t.Fatalf("expected 1 assigned, got %+v", byStatus)
	}

	byAgent, err := conn.Tasks.List(ctx, ports.TaskFilter{AssignedTo: &agent})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(byAgent) != 1 || byAgent[0].ID != tk2.ID {
		t.Fatalf("expected 1 by agent, got %+v", byAgent)
	}
}
