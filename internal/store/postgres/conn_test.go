package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/store/postgres"
)

// Postgres tests are integration tests: they require a reachable
// PostgreSQL instance and run only when NOUS_TEST_POSTGRES_DSN is
// set. Without the env var the suite skips, so unit-test runs in
// CI/IDE without a database stay green.
//
// Schema is created fresh per test process; tests share a database
// but operate on disjoint UUIDs, which is enough isolation for the
// CRUD coverage here.

func dsn(t *testing.T) string {
	t.Helper()
	v := os.Getenv("NOUS_TEST_POSTGRES_DSN")
	if v == "" {
		t.Skip("NOUS_TEST_POSTGRES_DSN unset; skipping postgres integration tests")
	}
	return v
}

func openTestConn(t *testing.T) *postgres.Conn {
	t.Helper()
	c, err := postgres.Open(dsn(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestPostgres_CommitmentRoundTrip(t *testing.T) {
	conn := openTestConn(t)
	ctx := context.Background()
	now := time.Now().UTC().Round(time.Microsecond) // pg time precision
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
	if got.Description != c.Description || len(got.SourceRefs) != 1 || got.SourceRefs[0].Locator != "evt-1" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}
