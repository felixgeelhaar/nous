package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/google/uuid"
)

type goalRepo struct {
	db *sql.DB
}

const upsertGoalSQL = `
INSERT INTO goals (
    id, owner_id, title, description, priority, status,
    source_refs, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9)
ON CONFLICT (id) DO UPDATE SET
    title       = EXCLUDED.title,
    description = EXCLUDED.description,
    priority    = EXCLUDED.priority,
    status      = EXCLUDED.status,
    source_refs = EXCLUDED.source_refs,
    updated_at  = EXCLUDED.updated_at;`

func (r *goalRepo) Save(ctx context.Context, g domain.Goal) error {
	if err := g.Validate(); err != nil {
		return err
	}
	sourceRefs, err := encJSON(g.SourceRefs)
	if err != nil {
		return fmt.Errorf("goal: encode source_refs: %w", err)
	}
	if _, err := r.db.ExecContext(ctx, upsertGoalSQL,
		g.ID, g.OwnerID, g.Title, g.Description,
		int(g.Priority), string(g.Status), sourceRefs,
		g.CreatedAt.UTC(), g.UpdatedAt.UTC(),
	); err != nil {
		return fmt.Errorf("goal: save: %w", err)
	}
	return nil
}

const selectGoalColumns = `
    id, owner_id, title, description, priority, status,
    source_refs, created_at, updated_at`

func (r *goalRepo) Get(ctx context.Context, id domain.GoalID) (domain.Goal, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+selectGoalColumns+` FROM goals WHERE id = $1`, id)
	g, err := scanGoal(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Goal{}, fmt.Errorf("%w: %s", domain.ErrGoalNotFound, id)
		}
		return domain.Goal{}, err
	}
	return g, nil
}

func (r *goalRepo) ListByOwner(ctx context.Context, ownerID string, limit int) ([]domain.Goal, error) {
	q := `SELECT ` + selectGoalColumns + `
        FROM goals
        WHERE owner_id = $1
        ORDER BY created_at DESC`
	args := []any{ownerID}
	if limit > 0 {
		q += " LIMIT $2"
		args = append(args, limit)
	}
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("goal: query: %w", err)
	}
	defer rows.Close()
	out := make([]domain.Goal, 0)
	for rows.Next() {
		g, err := scanGoal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func scanGoal(s rowScanner) (domain.Goal, error) {
	var (
		id         uuid.UUID
		priority   int
		statusStr  string
		sourceRefs []byte
		createdAt  time.Time
		updatedAt  time.Time
	)
	g := domain.Goal{}
	if err := s.Scan(
		&id, &g.OwnerID, &g.Title, &g.Description,
		&priority, &statusStr, &sourceRefs,
		&createdAt, &updatedAt,
	); err != nil {
		return domain.Goal{}, err
	}
	g.ID = id
	g.Priority = domain.Priority(priority)
	g.Status = domain.GoalStatus(statusStr)
	if err := decJSON(sourceRefs, &g.SourceRefs); err != nil {
		return domain.Goal{}, fmt.Errorf("goal: decode source_refs: %w", err)
	}
	g.CreatedAt = createdAt.UTC()
	g.UpdatedAt = updatedAt.UTC()
	return g, nil
}
