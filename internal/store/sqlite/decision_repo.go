package sqlite

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
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
         ON CONFLICT(id) DO NOTHING`,
		d.ID.String(), d.Subject, contextRefs, inputs, weights, outcome,
		d.Reason, d.Confidence, d.CreatedAt.UTC()); err != nil {
		return fmt.Errorf("decision: save: %w", err)
	}
	return nil
}

const selectDecisionColumns = `
    id, subject, context_refs, inputs, weights, outcome, reason, confidence, created_at`

func (r *decisionRepo) Get(ctx context.Context, id domain.DecisionID) (domain.Decision, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+selectDecisionColumns+` FROM decisions WHERE id = ?`,
		id.String())
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
        WHERE subject = ?
        ORDER BY created_at DESC`
	args := []any{subject}
	if limit > 0 {
		q += " LIMIT ?"
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
		idStr       string
		contextRefs string
		inputs      string
		weights     sql.NullString
		outcome     string
		createdAt   time.Time
	)
	d := domain.Decision{}
	if err := s.Scan(
		&idStr,
		&d.Subject,
		&contextRefs,
		&inputs,
		&weights,
		&outcome,
		&d.Reason,
		&d.Confidence,
		&createdAt,
	); err != nil {
		return domain.Decision{}, err
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return domain.Decision{}, fmt.Errorf("decision: invalid uuid %q: %w", idStr, err)
	}
	d.ID = id
	if err := decJSON(contextRefs, &d.ContextRefs); err != nil {
		return domain.Decision{}, fmt.Errorf("decision: decode context_refs: %w", err)
	}
	if err := decJSON(inputs, &d.Inputs); err != nil {
		return domain.Decision{}, fmt.Errorf("decision: decode inputs: %w", err)
	}
	if weights.Valid && weights.String != "" {
		if err := decJSON(weights.String, &d.Weights); err != nil {
			return domain.Decision{}, fmt.Errorf("decision: decode weights: %w", err)
		}
	}
	if err := decJSON(outcome, &d.Outcome); err != nil {
		return domain.Decision{}, fmt.Errorf("decision: decode outcome: %w", err)
	}
	d.CreatedAt = createdAt.UTC()
	return d, nil
}
