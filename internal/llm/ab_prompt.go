package llm

import (
	"context"
	"errors"
	"hash/fnv"
	"sort"
	"sync"

	"github.com/felixgeelhaar/nous/internal/domain"
)

// PromptVariant pairs a prompt builder with its experiment label and
// traffic weight. Weights are relative; the router normalises them.
type PromptVariant struct {
	// Label tags drafts emitted by this variant in the metadata so
	// downstream evals can compare.
	Label string
	// Weight controls the share of traffic. Variants with weight ≤ 0
	// are skipped.
	Weight  int
	Builder *PromptBuilder
}

// ABRouter is an extractor that splits traffic across PromptBuilders
// using a deterministic hash of the input's OwnerID. The same owner
// always sees the same variant — important for evaluation, since
// per-call random splits would mix experiment arms inside a single
// user's history.
type ABRouter struct {
	provider Provider
	budget   ContextBudget

	mu        sync.RWMutex
	variants  []PromptVariant
	total     int
	lastLabel string
}

// NewABRouter constructs an A/B router. variants must be non-empty
// and at least one must have weight > 0. The provider is shared
// across variants — A/B testing prompts, not providers.
func NewABRouter(provider Provider, variants []PromptVariant, opts ...LLMExtractorOption) (*ABRouter, error) {
	if provider == nil {
		return nil, errors.New("ab: provider required")
	}
	r := &ABRouter{provider: provider}
	for _, o := range opts {
		// Reuse LLMExtractorOption for budget. Builder option is
		// ignored — the router already owns its variants.
		fake := &LLMExtractor{}
		o(fake)
		if fake.budget.MaxInputTokens > 0 {
			r.budget = fake.budget
		}
	}
	if err := r.SetVariants(variants); err != nil {
		return nil, err
	}
	return r, nil
}

// SetVariants atomically swaps the variant set. Useful for hot
// reconfiguration (e.g., promoting a winning arm to 100% traffic).
func (r *ABRouter) SetVariants(variants []PromptVariant) error {
	if len(variants) == 0 {
		return errors.New("ab: at least one variant required")
	}
	cp := make([]PromptVariant, 0, len(variants))
	total := 0
	for _, v := range variants {
		if v.Weight <= 0 {
			continue
		}
		if v.Builder == nil {
			v.Builder = NewPromptBuilder()
		}
		cp = append(cp, v)
		total += v.Weight
	}
	if total == 0 {
		return errors.New("ab: total weight is zero")
	}
	// Sort by label for deterministic bucket boundaries — different
	// permutations of the same set must yield the same routing.
	sort.SliceStable(cp, func(i, j int) bool { return cp[i].Label < cp[j].Label })
	r.mu.Lock()
	r.variants = cp
	r.total = total
	r.mu.Unlock()
	return nil
}

// Extract picks a variant deterministically by hashing the OwnerID,
// runs extraction with that variant's prompt, and returns the
// drafts. Variant labels are not stamped onto draft fields here —
// the pipeline can read [LastVariant] right after the call.
func (r *ABRouter) Extract(ctx context.Context, in ExtractInput) ([]domain.CommitmentDraft, error) {
	v := r.pick(in.OwnerID)
	r.mu.Lock()
	r.lastLabel = v.Label
	r.mu.Unlock()

	text := in.Text
	if r.budget.MaxInputTokens > 0 {
		overhead := EstimateTokens(v.Builder.Build(""))
		text = FitText(text, overhead, r.budget)
	}
	prompt := v.Builder.Build(text)
	drafts, err := r.provider.ExtractCommitments(ctx, prompt)
	if err != nil {
		return nil, err
	}
	for i := range drafts {
		if drafts[i].OwnerID == "" {
			drafts[i].OwnerID = in.OwnerID
		}
		drafts[i].SourceRefs = append(append([]domain.SourceRef(nil), drafts[i].SourceRefs...), in.SourceRefs...)
	}
	return drafts, nil
}

// LastVariant returns the label of the variant most recently routed
// to. Useful in tests and for stamping run metadata.
func (r *ABRouter) LastVariant() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastLabel
}

// pick deterministically selects a variant for ownerID using FNV-1a
// hashing modulo total weight.
func (r *ABRouter) pick(ownerID string) PromptVariant {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.variants) == 1 {
		return r.variants[0]
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(ownerID))
	bucket := int(h.Sum64() % uint64(r.total))
	for _, v := range r.variants {
		if bucket < v.Weight {
			return v
		}
		bucket -= v.Weight
	}
	return r.variants[len(r.variants)-1] // unreachable absent integer overflow
}

// internal — set by Extract for [LastVariant].
func (r *ABRouter) lastLabelField() *string { return &r.lastLabel }

// (declared on the struct to avoid exporting a setter.)
//
//nolint:unused
var _ = (*ABRouter)(nil).lastLabelField
