package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// encJSON marshals v into a string suitable for storing in a TEXT
// column. Nil slices/maps round-trip as their JSON null/empty form,
// which keeps the column NOT NULL with sensible defaults.
func encJSON(v any) (string, error) {
	if v == nil {
		return "null", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("json marshal: %w", err)
	}
	return string(b), nil
}

// decJSON unmarshals a stored TEXT payload into out. Empty strings
// are treated as zero-value (the schema's defaults are '[]'/'{}'
// already, so this only fires for legacy/sparse rows).
func decJSON(raw string, out any) error {
	if raw == "" || raw == "null" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		return fmt.Errorf("json unmarshal: %w", err)
	}
	return nil
}

// nullTime turns a *time.Time into the database/sql NullTime type.
// The pipeline returns pointers; the driver wants NullTime.
func nullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t.UTC(), Valid: true}
}

// timePtr is the inverse of nullTime: NullTime back to *time.Time.
func timePtr(n sql.NullTime) *time.Time {
	if !n.Valid {
		return nil
	}
	t := n.Time.UTC()
	return &t
}

func stringPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	v := n.String
	return &v
}
