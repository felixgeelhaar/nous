// Package coordination is the Decision -> Action boundary of Nous.
//
// Nous decides; Praxis executes. This package owns the seam: it
// converts an accepted Intervention into a Praxis ActionRequest,
// invokes Praxis, captures the evidence, and writes that evidence
// back to Mnemos. Two kernel implementations are provided:
//
//   - DirectKernel: a thin pipeline that calls Praxis without an
//     execution harness. Useful for tests and minimal deployments.
//   - AxiKernel: wraps the same call in axi-go so write-external
//     actions get effect-gated approval, budgets, and a tamper-
//     evident evidence chain. Production wiring uses this.
//
// Both kernels expose the same Kernel interface so the application
// layer never has to care which one is wired.
package coordination

import (
	"context"
	"errors"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/ports"
)

// Kernel is the application-layer port. Given an accepted
// intervention with a SuggestedAction, the kernel runs the action
// through Praxis and returns the outcome. The implementation is
// responsible for evidence and for any approval gating.
type Kernel interface {
	Execute(ctx context.Context, iv domain.Intervention) (Outcome, error)
}

// Outcome captures the result of running an intervention's
// suggested action. Evidence carries structured records (operation
// counts, target IDs, audit hashes) that the pipeline forwards to
// Mnemos so future recalls can cite them.
type Outcome struct {
	InterventionID domain.InterventionID
	ActionRequest  domain.ActionRequest
	Success        bool
	Output         map[string]any
	ErrorMessage   string
	Evidence       []EvidenceRecord
}

// EvidenceRecord is the kernel-neutral projection of one piece of
// audit data. AxiKernel populates this from axi-go's own evidence
// chain; DirectKernel synthesises a single record from the Praxis
// response. Either way, the pipeline treats them identically when
// emitting Mnemos events.
type EvidenceRecord struct {
	Kind   string         `json:"kind"`
	Source string         `json:"source"`
	Value  map[string]any `json:"value"`
}

// ErrNoSuggestedAction is returned when the kernel is asked to
// execute an intervention that has no SuggestedAction. The pipeline
// should treat the intervention as informational-only and resolve
// it without invoking the kernel.
var ErrNoSuggestedAction = errors.New("coordination: intervention has no suggested action")

// requirePraxis is a small constructor guard reused by both
// kernels. Keeps the "we need a Praxis client" error message
// uniform.
func requirePraxis(p ports.PraxisClient) error {
	if p == nil {
		return errors.New("coordination: praxis client required")
	}
	return nil
}
