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

type interventionRepo struct {
	db *sql.DB
}

const upsertInterventionSQL = `
INSERT INTO interventions (
    id, commitment_id, task_id, type, message,
    suggested_action, status, triggered_at, resolved_at
) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9)
ON CONFLICT (id) DO UPDATE SET
    type             = EXCLUDED.type,
    message          = EXCLUDED.message,
    suggested_action = EXCLUDED.suggested_action,
    status           = EXCLUDED.status,
    resolved_at      = EXCLUDED.resolved_at;`

func (r *interventionRepo) Save(ctx context.Context, iv domain.Intervention) error {
	if err := iv.Validate(); err != nil {
		return err
	}
	var suggested any
	if iv.SuggestedAction != nil {
		b, err := encJSON(iv.SuggestedAction)
		if err != nil {
			return fmt.Errorf("intervention: encode suggested_action: %w", err)
		}
		suggested = b
	}
	if _, err := r.db.ExecContext(ctx, upsertInterventionSQL,
		iv.ID,
		nullUUID(iv.CommitmentID),
		nullUUID(iv.TaskID),
		string(iv.Type),
		iv.Message,
		suggested,
		string(iv.Status),
		iv.TriggeredAt.UTC(),
		nullTime(iv.ResolvedAt),
	); err != nil {
		return fmt.Errorf("intervention: save: %w", err)
	}
	return nil
}

const selectInterventionColumns = `
    id, commitment_id, task_id, type, message,
    suggested_action, status, triggered_at, resolved_at`

func (r *interventionRepo) Get(ctx context.Context, id domain.InterventionID) (domain.Intervention, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+selectInterventionColumns+` FROM interventions WHERE id = $1`, id)
	iv, err := scanIntervention(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Intervention{}, fmt.Errorf("%w: %s", domain.ErrInterventionNotFound, id)
		}
		return domain.Intervention{}, err
	}
	return iv, nil
}

func (r *interventionRepo) List(ctx context.Context, filter ports.InterventionFilter) ([]domain.Intervention, error) {
	var (
		clauses []string
		args    []any
	)
	param := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if filter.CommitmentID != nil {
		clauses = append(clauses, "commitment_id = "+param(*filter.CommitmentID))
	}
	if filter.TaskID != nil {
		clauses = append(clauses, "task_id = "+param(*filter.TaskID))
	}
	if len(filter.Statuses) > 0 {
		ph := make([]string, len(filter.Statuses))
		for i, s := range filter.Statuses {
			ph[i] = param(string(s))
		}
		clauses = append(clauses, "status IN ("+strings.Join(ph, ",")+")")
	}
	if filter.Since != nil {
		clauses = append(clauses, "triggered_at >= "+param(filter.Since.UTC()))
	}
	q := `SELECT ` + selectInterventionColumns + ` FROM interventions`
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	q += " ORDER BY triggered_at DESC"
	if filter.Limit > 0 {
		q += " LIMIT " + param(filter.Limit)
	}
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("intervention: query: %w", err)
	}
	defer rows.Close()
	out := make([]domain.Intervention, 0)
	for rows.Next() {
		iv, err := scanIntervention(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, iv)
	}
	return out, rows.Err()
}

func scanIntervention(s rowScanner) (domain.Intervention, error) {
	var (
		id           uuid.UUID
		commitmentNS sql.NullString
		taskNS       sql.NullString
		typeStr      string
		message      string
		suggested    []byte
		statusStr    string
		triggeredAt  time.Time
		resolvedAt   sql.NullTime
	)
	if err := s.Scan(
		&id, &commitmentNS, &taskNS, &typeStr, &message,
		&suggested, &statusStr, &triggeredAt, &resolvedAt,
	); err != nil {
		return domain.Intervention{}, err
	}
	iv := domain.Intervention{
		ID:          id,
		Type:        domain.InterventionType(typeStr),
		Message:     message,
		Status:      domain.InterventionStatus(statusStr),
		TriggeredAt: triggeredAt.UTC(),
		ResolvedAt:  timePtr(resolvedAt),
	}
	cid, err := uuidPtr(commitmentNS)
	if err != nil {
		return domain.Intervention{}, fmt.Errorf("intervention: commitment uuid: %w", err)
	}
	iv.CommitmentID = cid
	tid, err := uuidPtr(taskNS)
	if err != nil {
		return domain.Intervention{}, fmt.Errorf("intervention: task uuid: %w", err)
	}
	iv.TaskID = tid
	if len(suggested) > 0 && string(suggested) != "null" {
		var ar domain.ActionRequest
		if err := decJSON(suggested, &ar); err != nil {
			return domain.Intervention{}, fmt.Errorf("intervention: decode suggested_action: %w", err)
		}
		iv.SuggestedAction = &ar
	}
	return iv, nil
}
