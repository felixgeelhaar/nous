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

type decisionRepo struct {
	db *sql.DB
}

func (r *decisionRepo) Save(ctx context.Context, d domain.Decision) error {
	if err := d.Validate(); err != nil {
		return err
	}
	contextRefs, err := encJSON(d.ContextRefs)
	if err != nil {
		return fmt.Errorf("decision: encode context_refs: %w", err)
	}
	inputs, err := encJSON(d.Inputs)
	if err != nil {
		return fmt.Errorf("decision: encode inputs: %w", err)
	}
	outcome, err := encJSON(d.Outcome)
	if err != nil {
		return fmt.Errorf("decision: encode outcome: %w", err)
	}
	var weights any
	if len(d.Weights) > 0 {
		w, err := encJSON(d.Weights)
		if err != nil {
			return fmt.Errorf("decision: encode weights: %w", err)
		}
		weights = w
	}
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO decisions
            (id, subject, context_refs, inputs, weights, outcome, reason, confidence, created_at)
         VALUES ($1, $2, $3::jsonb, $4::jsonb, $5::jsonb, $6::jsonb, $7, $8, $9)
         ON CONFLICT (id) DO NOTHING`,
		d.ID, d.Subject, contextRefs, inputs, weights, outcome, d.Reason, d.Confidence, d.CreatedAt.UTC(),
	); err != nil {
		return fmt.Errorf("decision: save: %w", err)
	}
	return nil
}

const selectDecisionColumns = `
    id, subject, context_refs, inputs, weights, outcome, reason, confidence, created_at`

func (r *decisionRepo) Get(ctx context.Context, id domain.DecisionID) (domain.Decision, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+selectDecisionColumns+` FROM decisions WHERE id = $1`, id)
	d, err := scanDecision(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Decision{}, fmt.Errorf("%w: %s", domain.ErrDecisionNotFound, id)
		}
		return domain.Decision{}, err
	}
	return d, nil
}

func (r *decisionRepo) ListBySubject(ctx context.Context, subject string, limit int) ([]domain.Decision, error) {
	q := `SELECT ` + selectDecisionColumns + `
        FROM decisions
        WHERE subject = $1
        ORDER BY created_at DESC`
	args := []any{subject}
	if limit > 0 {
		q += " LIMIT $2"
		args = append(args, limit)
	}
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("decision: query: %w", err)
	}
	defer rows.Close()
	out := make([]domain.Decision, 0)
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func scanDecision(s rowScanner) (domain.Decision, error) {
	var (
		id          uuid.UUID
		contextRefs []byte
		inputs      []byte
		weights     []byte
		outcome     []byte
		createdAt   time.Time
	)
	d := domain.Decision{}
	if err := s.Scan(
		&id, &d.Subject, &contextRefs, &inputs, &weights, &outcome,
		&d.Reason, &d.Confidence, &createdAt,
	); err != nil {
		return domain.Decision{}, err
	}
	d.ID = id
	if err := decJSON(contextRefs, &d.ContextRefs); err != nil {
		return domain.Decision{}, fmt.Errorf("decision: decode context_refs: %w", err)
	}
	if err := decJSON(inputs, &d.Inputs); err != nil {
		return domain.Decision{}, fmt.Errorf("decision: decode inputs: %w", err)
	}
	if len(weights) > 0 {
		if err := decJSON(weights, &d.Weights); err != nil {
			return domain.Decision{}, fmt.Errorf("decision: decode weights: %w", err)
		}
	}
	if err := decJSON(outcome, &d.Outcome); err != nil {
		return domain.Decision{}, fmt.Errorf("decision: decode outcome: %w", err)
	}
	d.CreatedAt = createdAt.UTC()
	return d, nil
}
