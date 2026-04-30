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

type taskRepo struct {
	db *sql.DB
}

const upsertTaskSQL = `
INSERT INTO tasks (
    id, goal_id, plan_id, commitment_id,
    title, description, assigned_to,
    status, priority, due_at,
    created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    goal_id       = excluded.goal_id,
    plan_id       = excluded.plan_id,
    commitment_id = excluded.commitment_id,
    title         = excluded.title,
    description   = excluded.description,
    assigned_to   = excluded.assigned_to,
    status        = excluded.status,
    priority      = excluded.priority,
    due_at        = excluded.due_at,
    updated_at    = excluded.updated_at;`

func (r *taskRepo) Save(ctx context.Context, t domain.Task) error {
	if err := t.Validate(); err != nil {
		return err
	}
	var goalID, planID, commitmentID, assigned sql.NullString
	if t.GoalID != nil {
		goalID = sql.NullString{String: t.GoalID.String(), Valid: true}
	}
	if t.PlanID != nil {
		planID = sql.NullString{String: t.PlanID.String(), Valid: true}
	}
	if t.CommitmentID != nil {
		commitmentID = sql.NullString{String: t.CommitmentID.String(), Valid: true}
	}
	if t.AssignedTo != nil {
		assigned = sql.NullString{String: *t.AssignedTo, Valid: true}
	}
	if _, err := r.db.ExecContext(ctx, upsertTaskSQL,
		t.ID.String(),
		goalID, planID, commitmentID,
		t.Title, t.Description, assigned,
		string(t.Status), int(t.Priority), nullTime(t.DueAt),
		t.CreatedAt.UTC(), t.UpdatedAt.UTC(),
	); err != nil {
		return fmt.Errorf("task: save: %w", err)
	}
	return nil
}

const selectTaskColumns = `
    id, goal_id, plan_id, commitment_id,
    title, description, assigned_to,
    status, priority, due_at,
    created_at, updated_at`

func (r *taskRepo) Get(ctx context.Context, id domain.TaskID) (domain.Task, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+selectTaskColumns+` FROM tasks WHERE id = ?`,
		id.String())
	t, err := scanTask(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Task{}, fmt.Errorf("%w: %s", domain.ErrTaskNotFound, id)
		}
		return domain.Task{}, err
	}
	return t, nil
}

func (r *taskRepo) List(ctx context.Context, filter ports.TaskFilter) ([]domain.Task, error) {
	var (
		clauses []string
		args    []any
	)
	if filter.GoalID != nil {
		clauses = append(clauses, "goal_id = ?")
		args = append(args, filter.GoalID.String())
	}
	if filter.CommitmentID != nil {
		clauses = append(clauses, "commitment_id = ?")
		args = append(args, filter.CommitmentID.String())
	}
	if filter.AssignedTo != nil {
		clauses = append(clauses, "assigned_to = ?")
		args = append(args, *filter.AssignedTo)
	}
	if len(filter.Statuses) > 0 {
		placeholders := make([]string, len(filter.Statuses))
		for i, s := range filter.Statuses {
			placeholders[i] = "?"
			args = append(args, string(s))
		}
		clauses = append(clauses, "status IN ("+strings.Join(placeholders, ",")+")")
	}
	q := `SELECT ` + selectTaskColumns + ` FROM tasks`
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	q += " ORDER BY created_at ASC"
	if filter.Limit > 0 {
		q += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("task: query: %w", err)
	}
	defer rows.Close()
	out := make([]domain.Task, 0)
	for rows.Next() {
		tk, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, tk)
	}
	return out, rows.Err()
}

func scanTask(s rowScanner) (domain.Task, error) {
	var (
		idStr        string
		goalNS       sql.NullString
		planNS       sql.NullString
		commitmentNS sql.NullString
		assignedNS   sql.NullString
		statusStr    string
		priority     int
		dueAt        sql.NullTime
		createdAt    time.Time
		updatedAt    time.Time
	)
	t := domain.Task{}
	if err := s.Scan(
		&idStr,
		&goalNS, &planNS, &commitmentNS,
		&t.Title, &t.Description, &assignedNS,
		&statusStr, &priority, &dueAt,
		&createdAt, &updatedAt,
	); err != nil {
		return domain.Task{}, err
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return domain.Task{}, fmt.Errorf("task: invalid uuid %q: %w", idStr, err)
	}
	t.ID = id
	t.Status = domain.TaskStatus(statusStr)
	t.Priority = domain.Priority(priority)
	t.DueAt = timePtr(dueAt)
	t.CreatedAt = createdAt.UTC()
	t.UpdatedAt = updatedAt.UTC()
	if goalNS.Valid {
		gid, err := uuid.Parse(goalNS.String)
		if err != nil {
			return domain.Task{}, fmt.Errorf("task: invalid goal uuid: %w", err)
		}
		t.GoalID = &gid
	}
	if planNS.Valid {
		pid, err := uuid.Parse(planNS.String)
		if err != nil {
			return domain.Task{}, fmt.Errorf("task: invalid plan uuid: %w", err)
		}
		t.PlanID = &pid
	}
	if commitmentNS.Valid {
		cid, err := uuid.Parse(commitmentNS.String)
		if err != nil {
			return domain.Task{}, fmt.Errorf("task: invalid commitment uuid: %w", err)
		}
		t.CommitmentID = &cid
	}
	t.AssignedTo = stringPtr(assignedNS)
	return t, nil
}
