// Package intervention decides whether to interrupt the user (or
// trigger an automation) based on a risk assessment.
//
// The engine is pure: it does not save anything. The pipeline takes
// the engine's optional output and persists it through the
// repository. Keeping persistence out of this package means tests
// can validate the policy in isolation and the same engine works
// for dry-run / preview surfaces.
package intervention

import (
	"fmt"
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/risk"
)

// Config tunes the policy. Thresholds are intentionally simple: as
// soon as we have feedback data we'll bring in a more nuanced model
// (recency-weighted, owner-specific, etc.).
type Config struct {
	// NudgeThreshold is the risk score at or above which a nudge
	// should fire. Below this, the engine returns nil.
	NudgeThreshold float64
	// EscalateThreshold is the risk score at or above which an
	// escalation (rather than a nudge) should fire.
	EscalateThreshold float64
	// AutomationConfidence is the *commitment* confidence required
	// before the engine will suggest an automation rather than a
	// human-facing intervention. Low-confidence commitments don't
	// auto-trigger actions; we ask first.
	AutomationConfidence float64
}

// DefaultConfig returns sensible defaults for the MVP.
func DefaultConfig() Config {
	return Config{
		NudgeThreshold:       0.5,
		EscalateThreshold:    0.85,
		AutomationConfidence: 0.95,
	}
}

// Engine encapsulates the policy. Stateless beyond Config.
type Engine struct {
	cfg Config
}

// New returns an engine.
func New(cfg Config) *Engine { return &Engine{cfg: cfg} }

// MaybeIntervene returns a draft Intervention if the policy fires,
// or nil if the assessment doesn't warrant interruption. The
// returned intervention is *not* persisted; the pipeline calls Save.
//
// The shape of the message is informational only; the gRPC/HTTP
// layer can replace it with a localised version. We keep it human
// readable here so logs are debuggable without rendering.
func (e *Engine) MaybeIntervene(
	c domain.Commitment,
	assessment risk.RiskAssessment,
	now time.Time,
) (*domain.Intervention, error) {
	if assessment.Score < e.cfg.NudgeThreshold {
		return nil, nil
	}

	t := domain.InterventionNudge
	switch {
	case assessment.Score >= e.cfg.EscalateThreshold:
		t = domain.InterventionEscalation
	case assessment.Confidence >= e.cfg.AutomationConfidence && assessment.Score >= e.cfg.NudgeThreshold:
		t = domain.InterventionAutomation
	}

	msg := fmt.Sprintf(
		"%q is at risk of being missed (risk=%.2f, confidence=%.2f). %s",
		c.Description, assessment.Score, assessment.Confidence,
		topReason(assessment),
	)

	commitmentID := c.ID
	iv, err := domain.NewIntervention(t, msg, &commitmentID, nil, nil, now)
	if err != nil {
		return nil, fmt.Errorf("intervention: build: %w", err)
	}
	return &iv, nil
}

// topReason picks the highest-weight factor's reason for the
// human-readable message. Falls back to a generic line when the
// engine produced no factors (shouldn't happen above threshold).
func topReason(a risk.RiskAssessment) string {
	if len(a.Factors) == 0 {
		return "Risk threshold exceeded."
	}
	top := a.Factors[0]
	for _, f := range a.Factors[1:] {
		if f.Weight > top.Weight {
			top = f
		}
	}
	return top.Reason
}
