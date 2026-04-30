// Package llm holds the LLM-backed coordination surfaces of Nous.
//
// The package owns interfaces, not providers. Concrete adapters
// (agent-go scripted planner, agent-go + Anthropic LLM planner,
// OpenAI direct, etc.) live behind the [CommitmentExtractor] and
// [Planner] ports so the rest of the engine never imports an LLM
// SDK directly.
//
// Design rule: extractors and planners return *drafts*. Persistence,
// confidence thresholding, and lifecycle are pipeline concerns and
// live in the application layer.
package llm

import (
	"context"

	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/ports"
)

// ExtractInput carries the text Nous wants commitments extracted
// from, plus enough metadata that the pipeline can attribute the
// resulting drafts. OwnerID is the user the commitments belong to;
// SourceRefs travels through to every draft so audit can replay
// "where did this commitment come from".
type ExtractInput struct {
	OwnerID    string
	Text       string
	Hints      []string // optional ICL examples / domain hints
	SourceRefs []domain.SourceRef
}

// CommitmentExtractor is the port the pipeline depends on. The
// scripted implementation is deterministic and used in tests; the
// LLM-backed implementation lives in a sub-package and can be
// swapped at wiring time without touching pipeline code.
type CommitmentExtractor interface {
	Extract(ctx context.Context, in ExtractInput) ([]domain.CommitmentDraft, error)
}

// Planner converts a goal into a Plan. It is intentionally
// less-developed than the extractor in the MVP — we wire it once a
// goal-driven workflow is added. Declared here so the seam stays
// where outer code can already see it.
type Planner interface {
	CreatePlan(ctx context.Context, goal domain.Goal, ctxPacket ContextPacket) (domain.Plan, error)
}

// ContextPacket is a bag of supporting evidence the planner can
// use. Kept small on purpose — the planner should always run
// against compressed context, not raw event streams.
type ContextPacket struct {
	Memories []domain.MemoryRef
	Recent   []ports.MnemosEvent
}
