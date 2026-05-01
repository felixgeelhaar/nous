// Package postgres is the PostgreSQL-backed implementation of the
// Nous repositories.
//
// The schema mirrors the SQLite schema but uses jsonb, uuid, and
// timestamptz types directly. Driver: lib/pq via database/sql so we
// stay within the standard interfaces the engine already uses.
package postgres

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strings"

	// PostgreSQL driver registers itself under the name "postgres".
	_ "github.com/lib/pq"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open dials the supplied DSN (e.g. "postgres://user:pass@host/db"),
// pings to surface connectivity errors, and runs migrations
// transactionally. Idempotent migrations make repeated startups safe.
func Open(dsn string) (*Conn, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: open: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	if err := runMigrations(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("postgres: migrate: %w", err)
	}
	return newConn(db), nil
}

// runMigrations executes every embedded *.sql file in lexical order
// inside a single transaction. A `nous_schema_migrations` sentinel
// table tracks which files have already been applied so repeated
// Open() calls against a shared database don't replay migrations
// — concurrent runs would otherwise race on auto-created system
// types (pg_type_typname_nsp_index unique constraint).
func runMigrations(db *sql.DB) error {
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS nous_schema_migrations (
			name        text PRIMARY KEY,
			applied_at  timestamptz NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("create migrations bookkeeping: %w", err)
	}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		var applied bool
		if err := db.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM nous_schema_migrations WHERE name = $1)`,
			e.Name(),
		).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", e.Name(), err)
		}
		if applied {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return fmt.Errorf("read %s: %w", e.Name(), err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin: %w", err)
		}
		if _, err := tx.Exec(string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("exec %s: %w", e.Name(), err)
		}
		if _, err := tx.Exec(`INSERT INTO nous_schema_migrations (name) VALUES ($1)`, e.Name()); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record %s: %w", e.Name(), err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", e.Name(), err)
		}
	}
	return nil
}
