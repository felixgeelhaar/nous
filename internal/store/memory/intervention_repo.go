package memory

import (
	"context"
	"fmt"
	"sort"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/ports"
)

type interventionRepo struct {
	state *state
}

func (r *interventionRepo) Save(_ context.Context, iv domain.Intervention) error {
	if err := iv.Validate(); err != nil {
		return err
	}
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	r.state.interventions[iv.ID.String()] = storedIntervention{value: cloneIntervention(iv)}
	return nil
}

func (r *interventionRepo) Get(_ context.Context, id domain.InterventionID) (domain.Intervention, error) {
	r.state.mu.RLock()
	defer r.state.mu.RUnlock()
	row, ok := r.state.interventions[id.String()]
	if !ok {
		return domain.Intervention{}, fmt.Errorf("%w: %s", domain.ErrInterventionNotFound, id)
	}
	return cloneIntervention(row.value), nil
}

func (r *interventionRepo) List(_ context.Context, filter ports.InterventionFilter) ([]domain.Intervention, error) {
	r.state.mu.RLock()
	defer r.state.mu.RUnlock()
	out := make([]domain.Intervention, 0)
	for _, row := range r.state.interventions {
		if !matchesIntervention(row.value, filter) {
			continue
		}
		out = append(out, cloneIntervention(row.value))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].TriggeredAt.After(out[j].TriggeredAt)
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func matchesIntervention(iv domain.Intervention, f ports.InterventionFilter) bool {
	if f.CommitmentID != nil {
		if iv.CommitmentID == nil || *iv.CommitmentID != *f.CommitmentID {
			return false
		}
	}
	if f.TaskID != nil {
		if iv.TaskID == nil || *iv.TaskID != *f.TaskID {
			return false
		}
	}
	if len(f.Statuses) > 0 {
		match := false
		for _, s := range f.Statuses {
			if iv.Status == s {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}
	if f.Since != nil && iv.TriggeredAt.Before(*f.Since) {
		return false
	}
	return true
}
