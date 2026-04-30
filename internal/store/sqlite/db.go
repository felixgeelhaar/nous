// Package sqlite is the SQLite-backed implementation of the Nous
// repositories.
//
// PRAGMAs (foreign_keys, journal_mode=wal, busy_timeout) are encoded
// in the DSN we hand to the driver so callers don't need to remember
// to set them. Migrations live next to this file and are embedded
// at build time, so a freshly opened database is always at the
// expected schema.
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strings"

	// Pure-Go SQLite driver. No CGO requirement.
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// dsnPRAGMAs is appended to caller-supplied DSNs so we always run
// with the same pragmas in production. We keep the suffix small —
// callers needing exotic settings can pass a fully-formed DSN and
// disable this via the DSN's existing query string.
const dsnPRAGMAs = "_pragma=foreign_keys(1)&_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)"

// withPRAGMAs returns dsn with our standard pragmas appended unless
// the caller already specified pragmas (avoiding silent overrides).
func withPRAGMAs(dsn string) string {
	if strings.Contains(dsn, "_pragma=") {
		return dsn
	}
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	return dsn + separator + dsnPRAGMAs
}

// Open returns a Conn ready for use. Migrations run synchronously
// before the function returns so callers cannot accidentally hit a
// schema mismatch on first use.
func Open(dsn string) (*Conn, error) {
	db, err := sql.Open("sqlite", withPRAGMAs(dsn))
	if err != nil {
		return nil, fmt.Errorf("sqlite: open: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: ping: %w", err)
	}
	if err := runMigrations(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: migrate: %w", err)
	}
	return newConn(db), nil
}

// runMigrations executes every embedded *.sql file in lexical order
// inside a single transaction. Each migration is idempotent
// (CREATE TABLE IF NOT EXISTS), so repeated startups are safe.
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
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".sql") {
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
