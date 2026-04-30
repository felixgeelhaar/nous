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
// inside a single transaction.
func runMigrations(db *sql.DB) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("read %s: %w", e.Name(), err)
		}
		if _, err := tx.Exec(string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("exec %s: %w", e.Name(), err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
