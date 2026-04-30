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

type interventionRepo struct {
	db *sql.DB
}

const upsertInterventionSQL = `
INSERT INTO interventions (
    id, commitment_id, task_id, type, message,
    suggested_action, status, triggered_at, resolved_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    type             = excluded.type,
    message          = excluded.message,
    suggested_action = excluded.suggested_action,
    status           = excluded.status,
    resolved_at      = excluded.resolved_at;`

func (r *interventionRepo) Save(ctx context.Context, iv domain.Intervention) error {
	if err := iv.Validate(); err != nil {
		return err
	}
	suggested, err := encJSON(iv.SuggestedAction)
	if err != nil {
		return fmt.Errorf("intervention: encode suggested_action: %w", err)
	}
	suggestedNS := sql.NullString{String: suggested, Valid: iv.SuggestedAction != nil}

	var commitmentID, taskID sql.NullString
	if iv.CommitmentID != nil {
		commitmentID = sql.NullString{String: iv.CommitmentID.String(), Valid: true}
	}
	if iv.TaskID != nil {
		taskID = sql.NullString{String: iv.TaskID.String(), Valid: true}
	}

	if _, err := r.db.ExecContext(ctx, upsertInterventionSQL,
		iv.ID.String(),
		commitmentID,
		taskID,
		string(iv.Type),
		iv.Message,
		suggestedNS,
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
		`SELECT `+selectInterventionColumns+` FROM interventions WHERE id = ?`,
		id.String())
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
	if filter.CommitmentID != nil {
		clauses = append(clauses, "commitment_id = ?")
		args = append(args, filter.CommitmentID.String())
	}
	if filter.TaskID != nil {
		clauses = append(clauses, "task_id = ?")
		args = append(args, filter.TaskID.String())
	}
	if len(filter.Statuses) > 0 {
		placeholders := make([]string, len(filter.Statuses))
		for i, s := range filter.Statuses {
			placeholders[i] = "?"
			args = append(args, string(s))
		}
		clauses = append(clauses, "status IN ("+strings.Join(placeholders, ",")+")")
	}
	if filter.Since != nil {
		clauses = append(clauses, "triggered_at >= ?")
		args = append(args, filter.Since.UTC())
	}
	q := `SELECT ` + selectInterventionColumns + ` FROM interventions`
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	q += " ORDER BY triggered_at DESC"
	if filter.Limit > 0 {
		q += " LIMIT ?"
		args = append(args, filter.Limit)
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
		idStr        string
		commitmentNS sql.NullString
		taskNS       sql.NullString
		typeStr      string
		message      string
		suggestedNS  sql.NullString
		statusStr    string
		triggeredAt  time.Time
		resolvedAt   sql.NullTime
	)
	if err := s.Scan(
		&idStr,
		&commitmentNS,
		&taskNS,
		&typeStr,
		&message,
		&suggestedNS,
		&statusStr,
		&triggeredAt,
		&resolvedAt,
	); err != nil {
		return domain.Intervention{}, err
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return domain.Intervention{}, fmt.Errorf("intervention: invalid uuid %q: %w", idStr, err)
	}
	iv := domain.Intervention{
		ID:          id,
		Type:        domain.InterventionType(typeStr),
		Message:     message,
		Status:      domain.InterventionStatus(statusStr),
		TriggeredAt: triggeredAt.UTC(),
		ResolvedAt:  timePtr(resolvedAt),
	}
	if commitmentNS.Valid {
		cid, err := uuid.Parse(commitmentNS.String)
		if err != nil {
			return domain.Intervention{}, fmt.Errorf("intervention: invalid commitment uuid: %w", err)
		}
		iv.CommitmentID = &cid
	}
	if taskNS.Valid {
		tid, err := uuid.Parse(taskNS.String)
		if err != nil {
			return domain.Intervention{}, fmt.Errorf("intervention: invalid task uuid: %w", err)
		}
		iv.TaskID = &tid
	}
	if suggestedNS.Valid && suggestedNS.String != "" && suggestedNS.String != "null" {
		var ar domain.ActionRequest
		if err := decJSON(suggestedNS.String, &ar); err != nil {
			return domain.Intervention{}, fmt.Errorf("intervention: decode suggested_action: %w", err)
		}
		iv.SuggestedAction = &ar
	}
	return iv, nil
}
