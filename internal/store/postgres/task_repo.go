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

type taskRepo struct {
	db *sql.DB
}

const upsertTaskSQL = `
INSERT INTO tasks (
    id, goal_id, plan_id, commitment_id,
    title, description, assigned_to,
    status, priority, due_at,
    created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (id) DO UPDATE SET
    goal_id       = EXCLUDED.goal_id,
    plan_id       = EXCLUDED.plan_id,
    commitment_id = EXCLUDED.commitment_id,
    title         = EXCLUDED.title,
    description   = EXCLUDED.description,
    assigned_to   = EXCLUDED.assigned_to,
    status        = EXCLUDED.status,
    priority      = EXCLUDED.priority,
    due_at        = EXCLUDED.due_at,
    updated_at    = EXCLUDED.updated_at;`

func (r *taskRepo) Save(ctx context.Context, t domain.Task) error {
	if err := t.Validate(); err != nil {
		return err
	}
	if _, err := r.db.ExecContext(ctx, upsertTaskSQL,
		t.ID,
		nullUUID(t.GoalID),
		nullUUID(t.PlanID),
		nullUUID(t.CommitmentID),
		t.Title, t.Description, nullString(t.AssignedTo),
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
		`SELECT `+selectTaskColumns+` FROM tasks WHERE id = $1`, id)
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
	param := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if filter.GoalID != nil {
		clauses = append(clauses, "goal_id = "+param(*filter.GoalID))
	}
	if filter.CommitmentID != nil {
		clauses = append(clauses, "commitment_id = "+param(*filter.CommitmentID))
	}
	if filter.AssignedTo != nil {
		clauses = append(clauses, "assigned_to = "+param(*filter.AssignedTo))
	}
	if len(filter.Statuses) > 0 {
		ph := make([]string, len(filter.Statuses))
		for i, s := range filter.Statuses {
			ph[i] = param(string(s))
		}
		clauses = append(clauses, "status IN ("+strings.Join(ph, ",")+")")
	}
	q := `SELECT ` + selectTaskColumns + ` FROM tasks`
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	q += " ORDER BY created_at ASC"
	if filter.Limit > 0 {
		q += " LIMIT " + param(filter.Limit)
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
		id           uuid.UUID
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
		&id, &goalNS, &planNS, &commitmentNS,
		&t.Title, &t.Description, &assignedNS,
		&statusStr, &priority, &dueAt,
		&createdAt, &updatedAt,
	); err != nil {
		return domain.Task{}, err
	}
	t.ID = id
	t.Status = domain.TaskStatus(statusStr)
	t.Priority = domain.Priority(priority)
	t.DueAt = timePtr(dueAt)
	t.CreatedAt = createdAt.UTC()
	t.UpdatedAt = updatedAt.UTC()
	gid, err := uuidPtr(goalNS)
	if err != nil {
		return domain.Task{}, fmt.Errorf("task: goal uuid: %w", err)
	}
	t.GoalID = gid
	pid, err := uuidPtr(planNS)
	if err != nil {
		return domain.Task{}, fmt.Errorf("task: plan uuid: %w", err)
	}
	t.PlanID = pid
	cid, err := uuidPtr(commitmentNS)
	if err != nil {
		return domain.Task{}, fmt.Errorf("task: commitment uuid: %w", err)
	}
	t.CommitmentID = cid
	t.AssignedTo = stringPtr(assignedNS)
	return t, nil
}
