package coordination

import (
	"context"
	"fmt"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/ports"
)

// DirectKernel calls Praxis directly with no execution harness in
// front of it. We use it in tests, in environments where axi-go's
// approval/audit machinery is overkill, and as a fallback when the
// axi-go kernel is misconfigured (the failure mode then degrades
// to "still works, less audit" instead of "doesn't work").
type DirectKernel struct {
	praxis ports.PraxisClient
}

// NewDirectKernel returns a kernel that issues praxis.Execute for
// every intervention. Returns an error if praxis is nil — better to
// fail at wiring time than to NPE under load.
func NewDirectKernel(praxis ports.PraxisClient) (*DirectKernel, error) {
	if err := requirePraxis(praxis); err != nil {
		return nil, err
	}
	return &DirectKernel{praxis: praxis}, nil
}

// Execute runs iv.SuggestedAction through Praxis and converts the
// result into a kernel-neutral Outcome. Praxis errors are surfaced
// as Outcome.Success=false rather than Go errors so the pipeline
// can persist the failure path uniformly.
func (k *DirectKernel) Execute(ctx context.Context, iv domain.Intervention) (Outcome, error) {
	if iv.SuggestedAction == nil {
		return Outcome{InterventionID: iv.ID}, ErrNoSuggestedAction
	}
	req := *iv.SuggestedAction
	if req.IdempotencyKey == "" {
		// Default the idempotency key to the intervention id so
		// re-execution after a transient failure doesn't double up.
		req.IdempotencyKey = iv.ID.String()
	}

	res, err := k.praxis.Execute(ctx, req)
	if err != nil {
		return Outcome{
			InterventionID: iv.ID,
			ActionRequest:  req,
			Success:        false,
			ErrorMessage:   err.Error(),
			Evidence: []EvidenceRecord{{
				Kind:   "nous.action.error",
				Source: "nous.coordination.direct",
				Value: map[string]any{
					"capability": req.Capability,
					"error":      err.Error(),
				},
			}},
		}, fmt.Errorf("coordination: praxis execute: %w", err)
	}

	out := Outcome{
		InterventionID: iv.ID,
		ActionRequest:  req,
		Success:        res.Success,
		Output:         res.Output,
		ErrorMessage:   res.Error,
		Evidence: []EvidenceRecord{{
			Kind:   "nous.action.executed",
			Source: "nous.coordination.direct",
			Value: map[string]any{
				"capability":      res.Capability,
				"idempotency_key": res.IdempotencyKey,
				"success":         res.Success,
				"executed_at":     res.ExecutedAt,
			},
		}},
	}
	return out, nil
}
