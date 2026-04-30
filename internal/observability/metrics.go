package observability

import (
	"sync/atomic"
)

// Metrics holds Prometheus-style counters for the Nous engine.
// All fields are updated atomically and can be read safely from
// multiple goroutines.
type Metrics struct {
	commitmentsExtracted atomic.Uint64
	evaluationsRun       atomic.Uint64
	interventionsCreated atomic.Uint64
	riskDistribution     atomic.Pointer[map[string]uint64]
}

// NewMetrics returns a zero-valued Metrics ready for use.
func NewMetrics() *Metrics {
	m := &Metrics{}
	empty := make(map[string]uint64)
	m.riskDistribution.Store(&empty)
	return m
}

// IncCommitmentsExtracted increments the counter by n.
func (m *Metrics) IncCommitmentsExtracted(n uint64) {
	m.commitmentsExtracted.Add(n)
}

// IncEvaluationsRun increments the counter by n.
func (m *Metrics) IncEvaluationsRun(n uint64) {
	m.evaluationsRun.Add(n)
}

// IncInterventionsCreated increments the counter by n.
func (m *Metrics) IncInterventionsCreated(n uint64) {
	m.interventionsCreated.Add(n)
}

// RecordRiskBucket increments the bucket for the given risk label
// (e.g. "low", "medium", "high").
func (m *Metrics) RecordRiskBucket(label string) {
	for {
		oldPtr := m.riskDistribution.Load()
		old := *oldPtr
		next := make(map[string]uint64, len(old)+1)
		for k, v := range old {
			next[k] = v
		}
		next[label] = next[label] + 1
		if m.riskDistribution.CompareAndSwap(oldPtr, &next) {
			return
		}
	}
}

// Snapshot returns a stable copy of all counters.
func (m *Metrics) Snapshot() map[string]uint64 {
	return map[string]uint64{
		"commitments_extracted": m.commitmentsExtracted.Load(),
		"evaluations_run":       m.evaluationsRun.Load(),
		"interventions_created": m.interventionsCreated.Load(),
	}
}

// RiskDistribution returns a stable copy of the risk buckets.
func (m *Metrics) RiskDistribution() map[string]uint64 {
	old := *m.riskDistribution.Load()
	out := make(map[string]uint64, len(old))
	for k, v := range old {
		out[k] = v
	}
	return out
}
