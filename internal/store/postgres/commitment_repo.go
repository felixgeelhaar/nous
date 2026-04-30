package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
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
) VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (id) DO UPDATE SET
    owner_id          = EXCLUDED.owner_id,
    description       = EXCLUDED.description,
    source_refs       = EXCLUDED.source_refs,
    entities          = EXCLUDED.entities,
    due_at            = EXCLUDED.due_at,
    status            = EXCLUDED.status,
    confidence        = EXCLUDED.confidence,
    risk_score        = EXCLUDED.risk_score,
    last_evaluated_at = EXCLUDED.last_evaluated_at,
    updated_at        = EXCLUDED.updated_at;`

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
		c.ID,
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
		`SELECT `+selectCommitmentColumns+` FROM commitments WHERE id = $1`, id)
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
	param := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if filter.OwnerID != "" {
		clauses = append(clauses, "owner_id = "+param(filter.OwnerID))
	}
	if len(filter.Statuses) > 0 {
		ph := make([]string, len(filter.Statuses))
		for i, s := range filter.Statuses {
			ph[i] = param(string(s))
		}
		clauses = append(clauses, "status IN ("+strings.Join(ph, ",")+")")
	}
	if filter.DueBy != nil {
		clauses = append(clauses, "due_at IS NOT NULL AND due_at <= "+param(filter.DueBy.UTC()))
	}
	q := `SELECT ` + selectCommitmentColumns + ` FROM commitments`
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	q += " ORDER BY created_at ASC"
	if filter.Limit > 0 {
		q += " LIMIT " + param(filter.Limit)
	}
	return r.queryCommitments(ctx, q, args...)
}

func (r *commitmentRepo) FindActive(ctx context.Context, limit int) ([]domain.Commitment, error) {
	q := `SELECT ` + selectCommitmentColumns + `
        FROM commitments
        WHERE status IN ('pending','in_progress')
        ORDER BY risk_score DESC, due_at ASC NULLS LAST, created_at ASC`
	args := []any{}
	if limit > 0 {
		q += " LIMIT $1"
		args = append(args, limit)
	}
	return r.queryCommitments(ctx, q, args...)
}

func (r *commitmentRepo) UpdateRisk(ctx context.Context, id domain.CommitmentID, score float64, evaluatedAt time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE commitments
            SET risk_score = $2, last_evaluated_at = $3, updated_at = $3
            WHERE id = $1`,
		id, score, evaluatedAt.UTC())
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
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCommitment(s rowScanner) (domain.Commitment, error) {
	var (
		id         uuid.UUID
		sourceRefs []byte
		entities   []byte
		statusStr  string
		dueAt      sql.NullTime
		lastEvalAt sql.NullTime
		createdAt  time.Time
		updatedAt  time.Time
	)
	c := domain.Commitment{}
	if err := s.Scan(
		&id,
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
