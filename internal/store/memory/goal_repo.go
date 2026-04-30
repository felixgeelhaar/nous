package memory

import (
	"context"
	"fmt"
	"sort"

	"github.com/felixgeelhaar/nous/internal/domain"
)

type goalRepo struct {
	state *state
}

func (r *goalRepo) Save(_ context.Context, g domain.Goal) error {
	if err := g.Validate(); err != nil {
		return err
	}
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	r.state.goals[g.ID.String()] = storedGoal{value: cloneGoal(g)}
	return nil
}

func (r *goalRepo) Get(_ context.Context, id domain.GoalID) (domain.Goal, error) {
	r.state.mu.RLock()
	defer r.state.mu.RUnlock()
	row, ok := r.state.goals[id.String()]
	if !ok {
		return domain.Goal{}, fmt.Errorf("%w: %s", domain.ErrGoalNotFound, id)
	}
	return cloneGoal(row.value), nil
}

func (r *goalRepo) ListByOwner(_ context.Context, ownerID string, limit int) ([]domain.Goal, error) {
	r.state.mu.RLock()
	defer r.state.mu.RUnlock()
	out := make([]domain.Goal, 0)
	for _, row := range r.state.goals {
		if row.value.OwnerID != ownerID {
			continue
		}
		out = append(out, cloneGoal(row.value))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
