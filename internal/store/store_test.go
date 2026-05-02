package store_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/ports"
	"github.com/felixgeelhaar/nous/internal/store"
)

// backend describes one store implementation to parity-test.
type backend struct {
	name string
	open func() (*store.Conn, error)
}

// backends returns the list of backends to test. PostgreSQL is
// included only when NOUS_TEST_POSTGRES_DSN is set.
func backends(t *testing.T) []backend {
	t.Helper()
	out := []backend{
		{"memory", func() (*store.Conn, error) { return store.Open(context.Background(), "memory", "") }},
		{"sqlite", func() (*store.Conn, error) { return store.Open(context.Background(), "sqlite", ":memory:") }},
	}
	if dsn := os.Getenv("NOUS_TEST_POSTGRES_DSN"); dsn != "" {
		out = append(out, backend{"postgres", func() (*store.Conn, error) {
			// Parity tests share one Postgres database; subtests
			// inherit each other's rows otherwise. Truncate every
			// table the parity suite touches before each subtest
			// runs.
			if err := truncatePostgres(dsn); err != nil {
				return nil, err
			}
			return store.Open(context.Background(), "postgres", dsn)
		}})
	}
	return out
}

func truncatePostgres(dsn string) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(context.Background(),
		`TRUNCATE goals, commitments, tasks, interventions, decisions RESTART IDENTITY CASCADE`)
	return err
}

func TestParity_CommitmentCRUD(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	due := now.Add(24 * time.Hour)

	for _, be := range backends(t) {
		t.Run(be.name, func(t *testing.T) {
			conn, err := be.open()
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			ctx := context.Background()

			c, err := domain.NewCommitment("u1", "follow up", &due, 0.85, now)
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
			if got.OwnerID != "u1" || got.Description != "follow up" {
				t.Errorf("get mismatch: %+v", got)
			}
			if len(got.SourceRefs) != 1 || got.SourceRefs[0].Locator != "evt-1" {
				t.Errorf("source_refs round-trip failed: %+v", got.SourceRefs)
			}
			if len(got.Entities) != 1 || got.Entities[0].Name != "Alex" {
				t.Errorf("entities round-trip failed: %+v", got.Entities)
			}
			if got.DueAt == nil || !got.DueAt.Equal(due) {
				t.Errorf("due_at round-trip failed: got %v want %v", got.DueAt, due)
			}

			// UpdateRisk
			later := now.Add(time.Hour)
			if err := conn.Commitments.UpdateRisk(ctx, c.ID, 0.42, later); err != nil {
				t.Fatalf("update risk: %v", err)
			}
			got2, _ := conn.Commitments.Get(ctx, c.ID)
			if got2.RiskScore != 0.42 {
				t.Errorf("risk_score = %v, want 0.42", got2.RiskScore)
			}

			// FindActive ordering
			c2, _ := domain.NewCommitment("u1", "second", nil, 0.9, now.Add(time.Second))
			c2.RiskScore = 0.1
			if err := conn.Commitments.Save(ctx, c2); err != nil {
				t.Fatalf("save c2: %v", err)
			}
			active, err := conn.Commitments.FindActive(ctx, 0)
			if err != nil {
				t.Fatalf("find active: %v", err)
			}
			if len(active) != 2 {
				t.Fatalf("expected 2 active, got %d", len(active))
			}
			if active[0].RiskScore < active[1].RiskScore {
				t.Errorf("ordering wrong: %v then %v", active[0].RiskScore, active[1].RiskScore)
			}

			// List filter
			list, err := conn.Commitments.List(ctx, ports.CommitmentFilter{OwnerID: "u1"})
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(list) != 2 {
				t.Fatalf("expected 2, got %d", len(list))
			}

			// Get missing
			_, err = conn.Commitments.Get(ctx, domain.CommitmentID([16]byte{}))
			if !errors.Is(err, domain.ErrCommitmentNotFound) {
				t.Errorf("expected ErrCommitmentNotFound, got %v", err)
			}
		})
	}
}

func TestParity_DecisionCRUD(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	for _, be := range backends(t) {
		t.Run(be.name, func(t *testing.T) {
			conn, err := be.open()
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			ctx := context.Background()

			d, err := domain.NewDecision("commitment.extract", "extracted 2", 0.9, now)
			if err != nil {
				t.Fatalf("setup: %v", err)
			}
			d.Inputs = domain.DecisionInputs{"count": 2}
			d.Outcome = domain.DecisionOutcome{"saved": 2}

			if err := conn.Decisions.Save(ctx, d); err != nil {
				t.Fatalf("save: %v", err)
			}

			got, err := conn.Decisions.Get(ctx, d.ID)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if got.Subject != "commitment.extract" {
				t.Errorf("subject = %q, want commitment.extract", got.Subject)
			}
			countVal, ok := got.Inputs["count"]
			if !ok {
				t.Errorf("inputs missing 'count' key")
			} else if countVal != float64(2) && countVal != int(2) {
				// JSON backends decode numbers as float64; memory keeps the original int.
				t.Errorf("inputs round-trip failed: %+v", got.Inputs)
			}

			list, err := conn.Decisions.ListBySubject(ctx, "commitment.extract", 0)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(list) != 1 {
				t.Fatalf("expected 1, got %d", len(list))
			}

			_, err = conn.Decisions.Get(ctx, domain.DecisionID([16]byte{}))
			if !errors.Is(err, domain.ErrDecisionNotFound) {
				t.Errorf("expected ErrDecisionNotFound, got %v", err)
			}
		})
	}
}

func TestParity_InterventionCRUD(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	for _, be := range backends(t) {
		t.Run(be.name, func(t *testing.T) {
			conn, err := be.open()
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			ctx := context.Background()

			c, _ := domain.NewCommitment("u1", "task", nil, 0.9, now)
			if err := conn.Commitments.Save(ctx, c); err != nil {
				t.Fatalf("save commitment: %v", err)
			}

			action := &domain.ActionRequest{
				Capability:     "send_message",
				Payload:        map[string]any{"to": "alex"},
				IdempotencyKey: "key-1",
			}
			iv, err := domain.NewIntervention(domain.InterventionSuggestion, "ping", &c.ID, nil, action, now)
			if err != nil {
				t.Fatalf("setup: %v", err)
			}

			if err := conn.Interventions.Save(ctx, iv); err != nil {
				t.Fatalf("save: %v", err)
			}

			got, err := conn.Interventions.Get(ctx, iv.ID)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if got.SuggestedAction == nil || got.SuggestedAction.Capability != "send_message" {
				t.Errorf("suggested_action round-trip failed")
			}

			list, err := conn.Interventions.List(ctx, ports.InterventionFilter{CommitmentID: &c.ID})
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(list) != 1 {
				t.Fatalf("expected 1, got %d", len(list))
			}

			_, err = conn.Interventions.Get(ctx, domain.InterventionID([16]byte{}))
			if !errors.Is(err, domain.ErrInterventionNotFound) {
				t.Errorf("expected ErrInterventionNotFound, got %v", err)
			}
		})
	}
}

func TestStore_Open(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		dbType  string
		dsn     string
		wantErr bool
	}{
		{"memory", "memory", "", false},
		{"sqlite", "sqlite", ":memory:", false},
		{"sqlite3", "sqlite3", ":memory:", false},
		{"unsupported", "mongo", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := store.Open(context.Background(), tc.dbType, tc.dsn)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer func() { _ = conn.Close() }()

			if conn.Commitments == nil {
				t.Error("commitments repository is nil")
			}
			if conn.Decisions == nil {
				t.Error("decisions repository is nil")
			}
			if conn.Interventions == nil {
				t.Error("interventions repository is nil")
			}
			if conn.Goals == nil {
				t.Error("goals repository is nil")
			}
			if conn.Tasks == nil {
				t.Error("tasks repository is nil")
			}
		})
	}
}

func TestStore_SupportedTypes(t *testing.T) {
	types := store.SupportedTypes()
	if len(types) != 3 {
		t.Errorf("expected 3 supported types, got %d: %v", len(types), types)
	}
	seen := make(map[string]bool)
	for _, t := range types {
		seen[t] = true
	}
	for _, want := range []string{"memory", "sqlite", "postgres"} {
		if !seen[want] {
			t.Errorf("missing supported type: %s", want)
		}
	}
}

func TestStore_Close(t *testing.T) {
	t.Parallel()

	conn, err := store.Open(context.Background(), "memory", "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Errorf("close memory: %v", err)
	}

	conn, err = store.Open(context.Background(), "sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Errorf("close sqlite: %v", err)
	}
}

func TestStore_Ping(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	conn, err := store.Open(ctx, "memory", "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.Ping(ctx); err != nil {
		t.Errorf("ping memory: %v", err)
	}

	conn2, err := store.Open(ctx, "sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = conn2.Close() }()
	if err := conn2.Ping(ctx); err != nil {
		t.Errorf("ping sqlite: %v", err)
	}
}

func TestParity_GoalAndTaskCRUD(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	for _, be := range backends(t) {
		t.Run(be.name, func(t *testing.T) {
			conn, err := be.open()
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			ctx := context.Background()

			g, err := domain.NewGoal("u1", "Ship Q2", "summary", domain.PriorityHigh, now)
			if err != nil {
				t.Fatalf("setup goal: %v", err)
			}
			if err := conn.Goals.Save(ctx, g); err != nil {
				t.Fatalf("save goal: %v", err)
			}

			tk, err := domain.NewTask("draft", "intro", domain.PriorityNormal, nil, now)
			if err != nil {
				t.Fatalf("setup task: %v", err)
			}
			gid := g.ID
			tk.GoalID = &gid
			if err := conn.Tasks.Save(ctx, tk); err != nil {
				t.Fatalf("save task: %v", err)
			}

			goals, err := conn.Goals.ListByOwner(ctx, "u1", 0)
			if err != nil {
				t.Fatalf("list goals: %v", err)
			}
			if len(goals) != 1 || goals[0].ID != g.ID {
				t.Errorf("unexpected goals: %+v", goals)
			}

			tasks, err := conn.Tasks.List(ctx, ports.TaskFilter{GoalID: &gid})
			if err != nil {
				t.Fatalf("list tasks: %v", err)
			}
			if len(tasks) != 1 || tasks[0].ID != tk.ID {
				t.Errorf("unexpected tasks: %+v", tasks)
			}

			_, err = conn.Goals.Get(ctx, domain.GoalID([16]byte{}))
			if !errors.Is(err, domain.ErrGoalNotFound) {
				t.Errorf("expected ErrGoalNotFound, got %v", err)
			}
			_, err = conn.Tasks.Get(ctx, domain.TaskID([16]byte{}))
			if !errors.Is(err, domain.ErrTaskNotFound) {
				t.Errorf("expected ErrTaskNotFound, got %v", err)
			}
		})
	}
}
