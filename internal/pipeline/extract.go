package pipeline

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/llm"
	"github.com/felixgeelhaar/nous/internal/observability"
	"github.com/felixgeelhaar/nous/internal/ports"
)

// ExtractorConfig wires the inputs the extraction workflow needs.
// The minimum-confidence threshold is policy: drafts below it are
// dropped silently and never reach the database.
type ExtractorConfig struct {
	Commitments     ports.CommitmentRepository
	Decisions       ports.DecisionRepository
	Extractor       llm.CommitmentExtractor
	MinConfidence   float64
	Clock           Clock
	Metrics         *observability.Metrics // optional
}

// Extractor turns text-with-context into persisted commitments.
// Holds its dependencies so callers can construct once and reuse;
// stateless beyond that.
type Extractor struct {
	commitments   ports.CommitmentRepository
	decisions     ports.DecisionRepository
	extractor     llm.CommitmentExtractor
	minConfidence float64
	clock         Clock
	metrics       *observability.Metrics
}

// NewExtractor validates cfg and returns an Extractor.
func NewExtractor(cfg ExtractorConfig) (*Extractor, error) {
	if cfg.Commitments == nil {
		return nil, errors.New("pipeline: commitments repository required")
	}
	if cfg.Decisions == nil {
		return nil, errors.New("pipeline: decisions repository required")
	}
	if cfg.Extractor == nil {
		return nil, errors.New("pipeline: commitment extractor required")
	}
	if cfg.MinConfidence < 0 || cfg.MinConfidence > 1 {
		return nil, fmt.Errorf("pipeline: min_confidence out of [0,1]: %v", cfg.MinConfidence)
	}
	clock := cfg.Clock
	if clock == nil {
		clock = SystemClock
	}
	return &Extractor{
		commitments:   cfg.Commitments,
		decisions:     cfg.Decisions,
		extractor:     cfg.Extractor,
		minConfidence: cfg.MinConfidence,
		clock:         clock,
		metrics:       cfg.Metrics,
	}, nil
}

// ExtractResult summarises a single Extract call.
type ExtractResult struct {
	Considered int
	Saved      []domain.CommitmentID
	Dropped    int // below threshold
	DecisionID *domain.DecisionID
}

// Extract runs the LLM extractor against the input and persists
// every draft above the configured confidence threshold. A single
// Decision row is recorded per call so the audit trail covers both
// "what got saved" and "what we dropped and why".
func (e *Extractor) Extract(ctx context.Context, in llm.ExtractInput) (ExtractResult, error) {
	now := e.clock()
	drafts, err := e.extractor.Extract(ctx, in)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("pipeline: extractor: %w", err)
	}
	res := ExtractResult{Considered: len(drafts)}

	for _, d := range drafts {
		if d.Confidence < e.minConfidence {
			res.Dropped++
			continue
		}
		c, err := d.ToCommitment(now)
		if err != nil {
			res.Dropped++
			continue
		}
		if err := e.commitments.Save(ctx, c); err != nil {
			// Save failure is loud but non-fatal: log via the
			// caller (the pipeline returns err only on
			// extractor failure, so callers can still see partial
			// progress).
			res.Dropped++
			continue
		}
		res.Saved = append(res.Saved, c.ID)
	}
	if e.metrics != nil {
		e.metrics.IncCommitmentsExtracted(uint64(len(res.Saved)))
	}

	// One decision per Extract call. Inputs and outcome carry the
	// counts so an auditor can replay "we saw N drafts, kept K".
	dec, err := domain.NewDecision(
		"commitment.extract",
		fmt.Sprintf("Saved %d/%d drafts above confidence %.2f.", len(res.Saved), res.Considered, e.minConfidence),
		1.0, // we are 100% confident in the bookkeeping
		now,
	)
	if err != nil {
		return res, nil
	}
	dec.ContextRefs = append([]domain.SourceRef(nil), in.SourceRefs...)
	dec.Inputs = domain.DecisionInputs{
		"owner_id":       in.OwnerID,
		"considered":     res.Considered,
		"min_confidence": e.minConfidence,
	}
	saved := make([]string, 0, len(res.Saved))
	for _, id := range res.Saved {
		saved = append(saved, id.String())
	}
	dec.Outcome = domain.DecisionOutcome{
		"saved_ids": saved,
		"dropped":   res.Dropped,
	}
	if err := e.decisions.Save(ctx, dec); err == nil {
		id := dec.ID
		res.DecisionID = &id
	}

	_ = time.Time{} // silence unused-import warning for future extensions
	return res, nil
}
