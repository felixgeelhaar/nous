package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/ports"
	"github.com/google/uuid"
)

type commitmentRepo struct {
	db *sql.DB
}

const upsertCommitmentSQL = `
INSERT INTO commitments (
    id, owner_id, description, source_refs, entities, due_at,
    status, confidence, risk_score, last_evaluated_at,
    created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    owner_id          = excluded.owner_id,
    description       = excluded.description,
    source_refs       = excluded.source_refs,
    entities          = excluded.entities,
    due_at            = excluded.due_at,
    status            = excluded.status,
    confidence        = excluded.confidence,
    risk_score        = excluded.risk_score,
    last_evaluated_at = excluded.last_evaluated_at,
    updated_at        = excluded.updated_at;`

func (r *commitmentRepo) Save(ctx context.Context, c domain.Commitment) error {
	if err := c.Validate(); err != nil {
		return err
	}
	sourceRefs, err := encJSON(c.SourceRefs)
	if err != nil {
		return fmt.Errorf("commitment: encode source_refs: %w", err)
	}
	entities, err := encJSON(c.Entities)
	if err != nil {
		return fmt.Errorf("commitment: encode entities: %w", err)
	}
	if _, err := r.db.ExecContext(ctx, upsertCommitmentSQL,
		c.ID.String(),
		c.OwnerID,
		c.Description,
		sourceRefs,
		entities,
		nullTime(c.DueAt),
		string(c.Status),
		c.Confidence,
		c.RiskScore,
		nullTime(c.LastEvaluatedAt),
		c.CreatedAt.UTC(),
		c.UpdatedAt.UTC(),
	); err != nil {
		return fmt.Errorf("commitment: save: %w", err)
	}
	return nil
}

const selectCommitmentColumns = `
    id, owner_id, description, source_refs, entities, due_at,
    status, confidence, risk_score, last_evaluated_at,
    created_at, updated_at`

func (r *commitmentRepo) Get(ctx context.Context, id domain.CommitmentID) (domain.Commitment, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+selectCommitmentColumns+` FROM commitments WHERE id = ?`,
		id.String())
	c, err := scanCommitment(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Commitment{}, fmt.Errorf("%w: %s", domain.ErrCommitmentNotFound, id)
		}
		return domain.Commitment{}, err
	}
	return c, nil
}

func (r *commitmentRepo) List(ctx context.Context, filter ports.CommitmentFilter) ([]domain.Commitment, error) {
	var (
		clauses []string
		args    []any
	)
	if filter.OwnerID != "" {
		clauses = append(clauses, "owner_id = ?")
		args = append(args, filter.OwnerID)
	}
	if len(filter.Statuses) > 0 {
		placeholders := make([]string, len(filter.Statuses))
		for i, s := range filter.Statuses {
			placeholders[i] = "?"
			args = append(args, string(s))
		}
		clauses = append(clauses, "status IN ("+strings.Join(placeholders, ",")+")")
	}
	if filter.DueBy != nil {
		clauses = append(clauses, "due_at IS NOT NULL AND due_at <= ?")
		args = append(args, filter.DueBy.UTC())
	}
	q := `SELECT ` + selectCommitmentColumns + ` FROM commitments`
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	q += " ORDER BY created_at ASC"
	if filter.Limit > 0 {
		q += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	return r.queryCommitments(ctx, q, args...)
}

func (r *commitmentRepo) FindActive(ctx context.Context, limit int) ([]domain.Commitment, error) {
	q := `SELECT ` + selectCommitmentColumns + `
        FROM commitments
        WHERE status IN ('pending','in_progress')
        ORDER BY risk_score DESC,
                 CASE WHEN due_at IS NULL THEN 1 ELSE 0 END,
                 due_at ASC,
                 created_at ASC`
	args := []any{}
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	return r.queryCommitments(ctx, q, args...)
}

func (r *commitmentRepo) UpdateRisk(ctx context.Context, id domain.CommitmentID, score float64, evaluatedAt time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE commitments
            SET risk_score = ?, last_evaluated_at = ?, updated_at = ?
            WHERE id = ?`,
		score, evaluatedAt.UTC(), evaluatedAt.UTC(), id.String())
	if err != nil {
		return fmt.Errorf("commitment: update risk: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("commitment: update risk: rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %s", domain.ErrCommitmentNotFound, id)
	}
	return nil
}

func (r *commitmentRepo) queryCommitments(ctx context.Context, q string, args ...any) ([]domain.Commitment, error) {
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("commitment: query: %w", err)
	}
	defer rows.Close()
	out := make([]domain.Commitment, 0)
	for rows.Next() {
		c, err := scanCommitment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("commitment: iterate: %w", err)
	}
	return out, nil
}

// rowScanner is the surface shared by *sql.Row and *sql.Rows for
// our purposes. Lets scanCommitment be reused for Get and List.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanCommitment(s rowScanner) (domain.Commitment, error) {
	var (
		idStr        string
		sourceRefs   string
		entities     string
		statusStr    string
		dueAt        sql.NullTime
		lastEvalAt   sql.NullTime
		createdAt    time.Time
		updatedAt    time.Time
	)
	c := domain.Commitment{}
	if err := s.Scan(
		&idStr,
		&c.OwnerID,
		&c.Description,
		&sourceRefs,
		&entities,
		&dueAt,
		&statusStr,
		&c.Confidence,
		&c.RiskScore,
		&lastEvalAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return domain.Commitment{}, err
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return domain.Commitment{}, fmt.Errorf("commitment: invalid uuid %q: %w", idStr, err)
	}
	c.ID = id
	c.Status = domain.CommitmentStatus(statusStr)
	if err := decJSON(sourceRefs, &c.SourceRefs); err != nil {
		return domain.Commitment{}, fmt.Errorf("commitment: decode source_refs: %w", err)
	}
	if err := decJSON(entities, &c.Entities); err != nil {
		return domain.Commitment{}, fmt.Errorf("commitment: decode entities: %w", err)
	}
	c.DueAt = timePtr(dueAt)
	c.LastEvaluatedAt = timePtr(lastEvalAt)
	c.CreatedAt = createdAt.UTC()
	c.UpdatedAt = updatedAt.UTC()
	return c, nil
}
