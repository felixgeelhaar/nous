package memory

import (
	"context"
	"fmt"
	"sort"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/ports"
)

type taskRepo struct {
	state *state
}

func (r *taskRepo) Save(_ context.Context, t domain.Task) error {
	if err := t.Validate(); err != nil {
		return err
	}
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	r.state.tasks[t.ID.String()] = storedTask{value: cloneTask(t)}
	return nil
}

func (r *taskRepo) Get(_ context.Context, id domain.TaskID) (domain.Task, error) {
	r.state.mu.RLock()
	defer r.state.mu.RUnlock()
	row, ok := r.state.tasks[id.String()]
	if !ok {
		return domain.Task{}, fmt.Errorf("%w: %s", domain.ErrTaskNotFound, id)
	}
	return cloneTask(row.value), nil
}

func (r *taskRepo) List(_ context.Context, filter ports.TaskFilter) ([]domain.Task, error) {
	r.state.mu.RLock()
	defer r.state.mu.RUnlock()
	out := make([]domain.Task, 0)
	for _, row := range r.state.tasks {
		if !matchesTask(row.value, filter) {
			continue
		}
		out = append(out, cloneTask(row.value))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func matchesTask(t domain.Task, f ports.TaskFilter) bool {
	if f.GoalID != nil {
		if t.GoalID == nil || *t.GoalID != *f.GoalID {
			return false
		}
	}
	if f.CommitmentID != nil {
		if t.CommitmentID == nil || *t.CommitmentID != *f.CommitmentID {
			return false
		}
	}
	if f.AssignedTo != nil {
		if t.AssignedTo == nil || *t.AssignedTo != *f.AssignedTo {
			return false
		}
	}
	if len(f.Statuses) > 0 {
		match := false
		for _, s := range f.Statuses {
			if t.Status == s {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}
	return true
}
