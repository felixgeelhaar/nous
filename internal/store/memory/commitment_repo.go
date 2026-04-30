package memory

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/ports"
)

type commitmentRepo struct {
	state *state
}

func (r *commitmentRepo) Save(_ context.Context, c domain.Commitment) error {
	if err := c.Validate(); err != nil {
		return err
	}
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	r.state.commitments[c.ID.String()] = storedCommitment{value: cloneCommitment(c)}
	return nil
}

func (r *commitmentRepo) Get(_ context.Context, id domain.CommitmentID) (domain.Commitment, error) {
	r.state.mu.RLock()
	defer r.state.mu.RUnlock()
	row, ok := r.state.commitments[id.String()]
	if !ok {
		return domain.Commitment{}, fmt.Errorf("%w: %s", domain.ErrCommitmentNotFound, id)
	}
	return cloneCommitment(row.value), nil
}

func (r *commitmentRepo) List(_ context.Context, filter ports.CommitmentFilter) ([]domain.Commitment, error) {
	r.state.mu.RLock()
	defer r.state.mu.RUnlock()
	out := make([]domain.Commitment, 0, len(r.state.commitments))
	for _, row := range r.state.commitments {
		if !matchesCommitment(row.value, filter) {
			continue
		}
		out = append(out, cloneCommitment(row.value))
	}
	sort.Slice(out, func(i, j int) bool {
		// Default ordering: created_at asc (predictable for tests).
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (r *commitmentRepo) FindActive(_ context.Context, limit int) ([]domain.Commitment, error) {
	r.state.mu.RLock()
	defer r.state.mu.RUnlock()
	out := make([]domain.Commitment, 0, len(r.state.commitments))
	for _, row := range r.state.commitments {
		if !row.value.Status.IsActive() {
			continue
		}
		out = append(out, cloneCommitment(row.value))
	}
	sort.Slice(out, func(i, j int) bool {
		// risk_score desc, due_at asc nulls last, created_at asc.
		a, b := out[i], out[j]
		if a.RiskScore != b.RiskScore {
			return a.RiskScore > b.RiskScore
		}
		switch {
		case a.DueAt != nil && b.DueAt == nil:
			return true
		case a.DueAt == nil && b.DueAt != nil:
			return false
		case a.DueAt != nil && b.DueAt != nil && !a.DueAt.Equal(*b.DueAt):
			return a.DueAt.Before(*b.DueAt)
		}
		return a.CreatedAt.Before(b.CreatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *commitmentRepo) UpdateRisk(_ context.Context, id domain.CommitmentID, score float64, evaluatedAt time.Time) error {
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	row, ok := r.state.commitments[id.String()]
	if !ok {
		return fmt.Errorf("%w: %s", domain.ErrCommitmentNotFound, id)
	}
	c := row.value
	c.RiskScore = score
	t := evaluatedAt.UTC()
	c.LastEvaluatedAt = &t
	c.UpdatedAt = t
	r.state.commitments[id.String()] = storedCommitment{value: c}
	return nil
}

func matchesCommitment(c domain.Commitment, f ports.CommitmentFilter) bool {
	if f.OwnerID != "" && c.OwnerID != f.OwnerID {
		return false
	}
	if len(f.Statuses) > 0 {
		match := false
		for _, s := range f.Statuses {
			if c.Status == s {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}
	if f.DueBy != nil {
		if c.DueAt == nil || c.DueAt.After(*f.DueBy) {
			return false
		}
	}
	return true
}
