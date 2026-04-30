// Package risk computes the likelihood that a commitment or task
// will be missed. The engine is intentionally pure: it consumes a
// commitment plus optional Chronos signals and Mnemos memory
// references and returns a deterministic [RiskAssessment]. No
// repositories, no clocks read from the wall — the caller passes
// `now` in.
//
// MVP scoring uses three additive factors (overdue, due_soon,
// confidence) clamped to [0, 1]. Chronos signal and recency factors
// have hooks but only contribute weight when callers pass them in;
// the formula is straightforward to extend without touching call
// sites because the result carries the contributing factors.
package risk

import (
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
)

// Config tunes the engine. Defaults match the spec's MVP formula
// and can be overridden via DefaultConfig() + field assignment for
// experiments.
type Config struct {
	// OverdueWeight is added when DueAt is in the past.
	OverdueWeight float64
	// DueSoonWeight is added when DueAt is within DueSoonWindow.
	DueSoonWeight  float64
	DueSoonWindow  time.Duration
	// ConfidenceWeight scales Confidence into a contribution. The
	// premise is that high-confidence commitments are tracked more
	// strictly than ambiguous ones — a low-confidence draft missing
	// is less actionable.
	ConfidenceWeight float64
	// SignalWeight is the per-signal contribution (capped at one
	// signal worth) when Chronos returns matching signals.
	SignalWeight float64
}

// DefaultConfig returns the spec's MVP weights.
func DefaultConfig() Config {
	return Config{
		OverdueWeight:    0.6,
		DueSoonWeight:    0.3,
		DueSoonWindow:    2 * time.Hour,
		ConfidenceWeight: 0.2,
		SignalWeight:     0.15,
	}
}

// RiskFactor names a single contributing reason. Persisting these
// alongside the score lets us explain "why did Nous nudge?" without
// recomputing.
type RiskFactor struct {
	Type   string  `json:"type"`
	Weight float64 `json:"weight"`
	Reason string  `json:"reason"`
}

// RiskAssessment is the engine's output. Score is clamped to [0, 1];
// Confidence carries through the commitment's own confidence so
// downstream code can distinguish "we are sure this will be missed"
// from "we are unsure this commitment is even real".
type RiskAssessment struct {
	Score      float64
	Factors    []RiskFactor
	Confidence float64
}

// Engine is a pure scorer. It holds Config so callers can construct
// once and reuse. Stateless beyond the config so any number of
// goroutines can share an instance.
type Engine struct {
	cfg Config
}

// New returns an engine initialised with cfg.
func New(cfg Config) *Engine {
	return &Engine{cfg: cfg}
}

// EvaluateCommitment scores the given commitment relative to `now`,
// optionally enriched by Chronos signals. Memories are accepted for
// future enrichment (recurrence patterns, similar past failures);
// the MVP ignores them, but keeping the parameter on the signature
// avoids a breaking change when we wire it.
func (e *Engine) EvaluateCommitment(
	c domain.Commitment,
	signals []domain.ChronosSignal,
	_ []domain.MemoryRef,
	now time.Time,
) RiskAssessment {
	factors := make([]RiskFactor, 0, 4)
	score := 0.0

	if c.DueAt != nil {
		switch {
		case now.After(*c.DueAt):
			score += e.cfg.OverdueWeight
			factors = append(factors, RiskFactor{
				Type:   "overdue",
				Weight: e.cfg.OverdueWeight,
				Reason: "Commitment is past its due time.",
			})
		case c.DueAt.Sub(now) <= e.cfg.DueSoonWindow:
			score += e.cfg.DueSoonWeight
			factors = append(factors, RiskFactor{
				Type:   "due_soon",
				Weight: e.cfg.DueSoonWeight,
				Reason: "Commitment is due within the soon-window.",
			})
		}
	}

	if c.Confidence > 0 {
		contribution := c.Confidence * e.cfg.ConfidenceWeight
		score += contribution
		factors = append(factors, RiskFactor{
			Type:   "confidence",
			Weight: contribution,
			Reason: "Commitment is high-confidence; tracked strictly.",
		})
	}

	// Each Chronos signal adds at most SignalWeight and we cap
	// total signal contribution at one weight worth. A single
	// strong signal is enough to flip a borderline commitment;
	// piling on doesn't keep raising the score linearly.
	if len(signals) > 0 {
		contribution := e.cfg.SignalWeight
		score += contribution
		factors = append(factors, RiskFactor{
			Type:   "chronos_signal",
			Weight: contribution,
			Reason: "Chronos detected a temporal pattern matching this commitment.",
		})
	}

	if score > 1 {
		score = 1
	}
	if score < 0 {
		score = 0
	}

	return RiskAssessment{
		Score:      score,
		Factors:    factors,
		Confidence: c.Confidence,
	}
}
