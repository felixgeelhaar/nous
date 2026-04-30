package memory

import (
	"sync"

	"github.com/felixgeelhaar/nous/internal/domain"
)

// state holds the maps backing every repository. Splitting the
// shared state out into its own struct lets each repo file stay
// small and focused on its aggregate without leaking sync details
// into Conn.
type state struct {
	mu            sync.RWMutex
	commitments   map[string]storedCommitment
	decisions     map[string]storedDecision
	interventions map[string]storedIntervention
	goals         map[string]storedGoal
	tasks         map[string]storedTask
}

// stored* wrappers exist so we can copy-on-read without aliasing
// the caller's slice fields. Every aggregate gets a thin wrapper so
// extending the in-memory representation later (e.g. adding an
// indexed search column) doesn't churn call sites.

type storedCommitment struct {
	value domain.Commitment
}

type storedDecision struct {
	value domain.Decision
}

type storedIntervention struct {
	value domain.Intervention
}

type storedGoal struct {
	value domain.Goal
}

type storedTask struct {
	value domain.Task
}

// cloneCommitment returns a defensive copy so neither the caller
// nor the store can observe a mutation through the other.
func cloneCommitment(c domain.Commitment) domain.Commitment {
	out := c
	out.SourceRefs = append([]domain.SourceRef(nil), c.SourceRefs...)
	out.Entities = append([]domain.EntityRef(nil), c.Entities...)
	if c.DueAt != nil {
		t := *c.DueAt
		out.DueAt = &t
	}
	if c.LastEvaluatedAt != nil {
		t := *c.LastEvaluatedAt
		out.LastEvaluatedAt = &t
	}
	return out
}

func cloneDecision(d domain.Decision) domain.Decision {
	out := d
	out.ContextRefs = append([]domain.SourceRef(nil), d.ContextRefs...)
	if d.Inputs != nil {
		out.Inputs = make(domain.DecisionInputs, len(d.Inputs))
		for k, v := range d.Inputs {
			out.Inputs[k] = v
		}
	}
	if d.Outcome != nil {
		out.Outcome = make(domain.DecisionOutcome, len(d.Outcome))
		for k, v := range d.Outcome {
			out.Outcome[k] = v
		}
	}
	return out
}

func cloneIntervention(iv domain.Intervention) domain.Intervention {
	out := iv
	if iv.SuggestedAction != nil {
		ar := *iv.SuggestedAction
		ar.Constraints = append([]domain.ActionConstraint(nil), iv.SuggestedAction.Constraints...)
		if iv.SuggestedAction.Payload != nil {
			ar.Payload = make(map[string]any, len(iv.SuggestedAction.Payload))
			for k, v := range iv.SuggestedAction.Payload {
				ar.Payload[k] = v
			}
		}
		out.SuggestedAction = &ar
	}
	if iv.ResolvedAt != nil {
		t := *iv.ResolvedAt
		out.ResolvedAt = &t
	}
	if iv.CommitmentID != nil {
		id := *iv.CommitmentID
		out.CommitmentID = &id
	}
	if iv.TaskID != nil {
		id := *iv.TaskID
		out.TaskID = &id
	}
	return out
}

func cloneGoal(g domain.Goal) domain.Goal {
	out := g
	out.SourceRefs = append([]domain.SourceRef(nil), g.SourceRefs...)
	return out
}

func cloneTask(t domain.Task) domain.Task {
	out := t
	if t.GoalID != nil {
		id := *t.GoalID
		out.GoalID = &id
	}
	if t.PlanID != nil {
		id := *t.PlanID
		out.PlanID = &id
	}
	if t.CommitmentID != nil {
		id := *t.CommitmentID
		out.CommitmentID = &id
	}
	if t.AssignedTo != nil {
		s := *t.AssignedTo
		out.AssignedTo = &s
	}
	if t.DueAt != nil {
		due := *t.DueAt
		out.DueAt = &due
	}
	return out
}
