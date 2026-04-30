// Package pipeline orchestrates the cross-aggregate workflows of the
// Nous engine. Each workflow is a struct so that wiring (clocks,
// loggers, repositories, clients) is explicit at construction and
// the methods take only the per-call inputs.
//
// `EvaluateCommitments` is the central worker loop: load active
// commitments, fetch supporting evidence, compute risk, persist the
// updated score, and (optionally) emit an intervention. It is pure
// in the sense that it produces a deterministic Result given the
// same inputs and clock — repository writes happen, but no
// uncontrolled I/O.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/intervention"
	"github.com/felixgeelhaar/nous/internal/observability"
	"github.com/felixgeelhaar/nous/internal/ports"
	"github.com/felixgeelhaar/nous/internal/risk"
)

// Clock is the time source used by the pipeline. Tests inject a
// frozen clock so behaviour is reproducible.
type Clock func() time.Time

// SystemClock returns time.Now in UTC.
func SystemClock() time.Time { return time.Now().UTC() }

// EvaluateOptions tunes a single Evaluate call.
type EvaluateOptions struct {
	// Limit caps the number of commitments processed in a single
	// call. 0 means no limit; production schedulers should set a
	// sensible cap so a misbehaving evaluator can't monopolise the
	// worker.
	Limit int
	// SignalLookback bounds the Chronos query. If zero, the
	// pipeline default (72h) is used.
	SignalLookback time.Duration
}

// Evaluator wires the dependencies needed by EvaluateCommitments.
// Construction-time validation gives clear failures up front instead
// of a NPE deep in the loop.
type Evaluator struct {
	commitments   ports.CommitmentRepository
	decisions     ports.DecisionRepository
	interventions ports.InterventionRepository
	mnemos        ports.MnemosClient
	chronos       ports.ChronosClient
	risk          *risk.Engine
	intervention  *intervention.Engine
	clock         Clock
	metrics       *observability.Metrics
}

// EvaluatorConfig is the constructor input.
type EvaluatorConfig struct {
	Commitments   ports.CommitmentRepository
	Decisions     ports.DecisionRepository
	Interventions ports.InterventionRepository
	Mnemos        ports.MnemosClient
	Chronos       ports.ChronosClient
	Risk          *risk.Engine
	Intervention  *intervention.Engine
	Clock         Clock // optional; defaults to SystemClock
	Metrics       *observability.Metrics // optional
}

// NewEvaluator validates cfg and returns an Evaluator. Mnemos and
// Chronos clients are optional (nil falls back to "no enrichment");
// the rest are required.
func NewEvaluator(cfg EvaluatorConfig) (*Evaluator, error) {
	if cfg.Commitments == nil {
		return nil, errors.New("pipeline: commitments repository required")
	}
	if cfg.Decisions == nil {
		return nil, errors.New("pipeline: decisions repository required")
	}
	if cfg.Interventions == nil {
		return nil, errors.New("pipeline: interventions repository required")
	}
	if cfg.Risk == nil {
		return nil, errors.New("pipeline: risk engine required")
	}
	if cfg.Intervention == nil {
		return nil, errors.New("pipeline: intervention engine required")
	}
	clock := cfg.Clock
	if clock == nil {
		clock = SystemClock
	}
	return &Evaluator{
		commitments:   cfg.Commitments,
		decisions:     cfg.Decisions,
		interventions: cfg.Interventions,
		mnemos:        cfg.Mnemos,
		chronos:       cfg.Chronos,
		risk:          cfg.Risk,
		intervention:  cfg.Intervention,
		clock:         clock,
		metrics:       cfg.Metrics,
	}, nil
}

// EvaluateResult summarises one run for the caller (HTTP handler,
// worker tick, CLI). The slice fields carry IDs not full records so
// the result is cheap to log.
type EvaluateResult struct {
	Evaluated     int
	Updated       int
	Interventions []domain.InterventionID
	Decisions     []domain.DecisionID
}

// Evaluate runs the active-commitment evaluation loop once.
//
// Steps:
//  1. Load active commitments (pending + in_progress).
//  2. For each, gather Mnemos memories + Chronos signals.
//  3. Score risk.
//  4. Persist the new score and a Decision recording why.
//  5. Ask the intervention engine; if it fires, persist the
//     intervention.
//
// The function does not stop at the first error: persistence
// failures for a single commitment are logged and the loop
// continues. This keeps a single bad row from stalling the worker.
func (e *Evaluator) Evaluate(ctx context.Context, opts EvaluateOptions) (EvaluateResult, error) {
	if e.metrics != nil {
		e.metrics.IncEvaluationsRun("success", 1)
	}

	ctx, span := observability.Tracer.Start(ctx, "pipeline.Evaluate")
	defer span.End()

	result := EvaluateResult{}
	now := e.clock()

	commitments, err := e.commitments.FindActive(ctx, opts.Limit)
	if err != nil {
		return result, fmt.Errorf("pipeline: find active: %w", err)
	}
	result.Evaluated = len(commitments)

	for _, c := range commitments {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		signals := e.fetchSignals(ctx, c, opts.SignalLookback, now)
		memories := e.fetchMemories(ctx, c, now)

		assessment := e.risk.EvaluateCommitment(c, signals, memories, now)
		if e.metrics != nil {
			e.metrics.ObserveRiskScore("scored", assessment.Score)
		}

		if err := e.commitments.UpdateRisk(ctx, c.ID, assessment.Score, now); err != nil {
			// Persist failure is loud but non-fatal: continue with
			// the next commitment.
			continue
		}
		result.Updated++

		dec, err := e.recordDecision(ctx, c, assessment, signals, now)
		if err == nil {
			result.Decisions = append(result.Decisions, dec.ID)
		}

		iv, err := e.intervention.MaybeIntervene(c, assessment, now)
		if err != nil || iv == nil {
			continue
		}
		if err := e.interventions.Save(ctx, *iv); err != nil {
			continue
		}
		result.Interventions = append(result.Interventions, iv.ID)
		if e.metrics != nil {
			e.metrics.IncInterventionsCreated("created", 1)
		}
	}

	return result, nil
}

func riskLabel(score float64) string {
	switch {
	case score < 0.4:
		return "low"
	case score < 0.7:
		return "medium"
	default:
		return "high"
	}
}

// fetchSignals queries Chronos when a client is configured. Soft-
// fail: an unreachable Chronos shouldn't block evaluation.
func (e *Evaluator) fetchSignals(ctx context.Context, c domain.Commitment, lookback time.Duration, now time.Time) []domain.ChronosSignal {
	if e.chronos == nil {
		return nil
	}
	if lookback == 0 {
		lookback = 72 * time.Hour
	}
	since := now.Add(-lookback)
	sigs, err := e.chronos.GetSignals(ctx, ports.SignalFilter{
		EntityRefs: c.Entities,
		Since:      &since,
	})
	if err != nil {
		return nil
	}
	return sigs
}

// fetchMemories pulls supporting Mnemos memories. Soft-fail.
func (e *Evaluator) fetchMemories(ctx context.Context, c domain.Commitment, _ time.Time) []domain.MemoryRef {
	if e.mnemos == nil {
		return nil
	}
	mems, err := e.mnemos.Recall(ctx, ports.RecallQuery{
		Text:     c.Description,
		Entities: c.Entities,
		Limit:    10,
	})
	if err != nil {
		return nil
	}
	return mems
}

// recordDecision writes one Decision per commitment per evaluation
// pass. The inputs and outcome maps are intentionally lightweight —
// the durable detail lives in the source refs and the risk factors.
func (e *Evaluator) recordDecision(
	ctx context.Context,
	c domain.Commitment,
	a risk.RiskAssessment,
	signals []domain.ChronosSignal,
	now time.Time,
) (domain.Decision, error) {
	dec, err := domain.NewDecision("commitment.evaluate", "Risk evaluated against current signals.", a.Confidence, now)
	if err != nil {
		return domain.Decision{}, err
	}
	dec.ContextRefs = append([]domain.SourceRef(nil), c.SourceRefs...)
	dec.Inputs = domain.DecisionInputs{
		"commitment_id": c.ID.String(),
		"signal_count":  len(signals),
	}
	dec.Outcome = domain.DecisionOutcome{
		"risk_score": a.Score,
		"factors":    a.Factors,
	}
	if err := e.decisions.Save(ctx, dec); err != nil {
		return domain.Decision{}, err
	}

	// Mirror the decision in Mnemos so it becomes evidence for future
	// recalls. Soft-fail: Mnemos being down must not stall evaluation.
	if e.mnemos != nil {
		_, _ = e.mnemos.AppendEvent(ctx, ports.MnemosEvent{
			Kind:       "nous.decision",
			OccurredAt: now,
			Actor:      "nous",
			Subject:    c.ID.String(),
			Body: map[string]any{
				"decision_id": dec.ID.String(),
				"risk_score":  a.Score,
				"confidence":  a.Confidence,
				"signal_count": len(signals),
			},
			SourceRefs: append([]domain.SourceRef(nil), dec.ContextRefs...),
		})
	}

	return dec, nil
}
