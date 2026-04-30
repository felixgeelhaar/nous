package postgres

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// encJSON marshals v for storage in a jsonb column. The driver
// accepts []byte for jsonb writes; we surface marshal errors so a
// caller bug doesn't hit the database silently.
func encJSON(v any) ([]byte, error) {
	if v == nil {
		return []byte("null"), nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("json marshal: %w", err)
	}
	return b, nil
}

// decJSON unmarshals a jsonb payload (read as []byte) into out.
// Empty/null payloads are treated as zero-value.
func decJSON(raw []byte, out any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("json unmarshal: %w", err)
	}
	return nil
}

// nullTime turns a *time.Time into sql.NullTime.
func nullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t.UTC(), Valid: true}
}

// timePtr is the inverse of nullTime.
func timePtr(n sql.NullTime) *time.Time {
	if !n.Valid {
		return nil
	}
	t := n.Time.UTC()
	return &t
}

// nullUUID converts a *uuid.UUID into sql.NullString carrying the
// canonical hyphenated form. lib/pq can scan uuid columns into
// strings without a custom driver type.
func nullUUID(id *uuid.UUID) sql.NullString {
	if id == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: id.String(), Valid: true}
}

// uuidPtr parses a sql.NullString back into a *uuid.UUID.
func uuidPtr(n sql.NullString) (*uuid.UUID, error) {
	if !n.Valid {
		return nil, nil
	}
	id, err := uuid.Parse(n.String)
	if err != nil {
		return nil, fmt.Errorf("parse uuid: %w", err)
	}
	return &id, nil
}

// nullString helpers for *string columns.
func nullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func stringPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	v := n.String
	return &v
}
