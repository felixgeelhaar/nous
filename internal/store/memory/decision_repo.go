package memory

import (
	"context"
	"fmt"
	"sort"

	"github.com/felixgeelhaar/nous/internal/domain"
)

type decisionRepo struct {
	state *state
}

func (r *decisionRepo) Save(_ context.Context, d domain.Decision) error {
	if err := d.Validate(); err != nil {
		return err
	}
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	r.state.decisions[d.ID.String()] = storedDecision{value: cloneDecision(d)}
	return nil
}

func (r *decisionRepo) Get(_ context.Context, id domain.DecisionID) (domain.Decision, error) {
	r.state.mu.RLock()
	defer r.state.mu.RUnlock()
	row, ok := r.state.decisions[id.String()]
	if !ok {
		return domain.Decision{}, fmt.Errorf("%w: %s", domain.ErrDecisionNotFound, id)
	}
	return cloneDecision(row.value), nil
}

func (r *decisionRepo) ListBySubject(_ context.Context, subject string, limit int) ([]domain.Decision, error) {
	r.state.mu.RLock()
	defer r.state.mu.RUnlock()
	out := make([]domain.Decision, 0)
	for _, row := range r.state.decisions {
		if row.value.Subject != subject {
			continue
		}
		out = append(out, cloneDecision(row.value))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
