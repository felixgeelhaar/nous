package observability

import (
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all Prometheus metrics for the Nous engine.
type Metrics struct {
	// Counters
	commitmentsExtracted *prometheus.CounterVec
	evaluationsRun     *prometheus.CounterVec
	interventionsCreated *prometheus.CounterVec

	// Histograms
	httpDuration   *prometheus.HistogramVec
	grpcDuration  *prometheus.HistogramVec
	riskScore     *prometheus.HistogramVec

	// Gauges: adapter health (1 = healthy, 0.5 = degraded, 0 = unhealthy)
	mnemosHealth  *prometheus.GaugeVec
	chronosHealth *prometheus.GaugeVec

	registry *prometheus.Registry
}

// NewMetrics creates and registers all Prometheus metrics.
func NewMetrics() *Metrics {
	m := &Metrics{}

	m.commitmentsExtracted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nous_commitments_extracted_total",
			Help: "Total number of commitments extracted",
		},
		[]string{"status"},
	)
	m.evaluationsRun = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nous_evaluations_run_total",
			Help: "Total number of evaluations run",
		},
		[]string{"status"},
	)
	m.interventionsCreated = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nous_interventions_created_total",
			Help: "Total number of interventions created",
		},
		[]string{"action"},
	)
	m.httpDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nous_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path", "status"},
	)
	m.grpcDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nous_grpc_request_duration_seconds",
			Help:    "gRPC request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "status"},
	)
	m.riskScore = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nous_risk_score",
			Help:    "Risk score distribution",
			Buckets: []float64{0.1, 0.3, 0.5, 0.7, 0.9, 1.0},
		},
		[]string{"outcome"},
	)
	m.mnemosHealth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nous_mnemos_adapter_health",
			Help: "Mnemos adapter health status (1=healthy, 0.5=degraded, 0=unhealthy)",
		},
		[]string{"adapter"},
	)
	m.chronosHealth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nous_chronos_adapter_health",
			Help: "Chronos adapter health status (1=healthy, 0.5=degraded, 0=unhealthy)",
		},
		[]string{"adapter"},
	)

	m.registry = prometheus.NewRegistry()
	m.registry.MustRegister(m.commitmentsExtracted)
	m.registry.MustRegister(m.evaluationsRun)
	m.registry.MustRegister(m.interventionsCreated)
	m.registry.MustRegister(m.httpDuration)
	m.registry.MustRegister(m.grpcDuration)
	m.registry.MustRegister(m.riskScore)
	m.registry.MustRegister(m.mnemosHealth)
	m.registry.MustRegister(m.chronosHealth)

	return m
}

// IncCommitmentsExtracted increments the counter.
func (m *Metrics) IncCommitmentsExtracted(status string, n int) {
	m.commitmentsExtracted.WithLabelValues(status).Add(float64(n))
}

// IncEvaluationsRun increments the counter.
func (m *Metrics) IncEvaluationsRun(status string, n int) {
	m.evaluationsRun.WithLabelValues(status).Add(float64(n))
}

// IncInterventionsCreated increments the counter.
func (m *Metrics) IncInterventionsCreated(action string, n int) {
	m.interventionsCreated.WithLabelValues(action).Add(float64(n))
}

// ObserveHTTPDuration records an HTTP request duration.
func (m *Metrics) ObserveHTTPDuration(method, path, status string, duration float64) {
	m.httpDuration.WithLabelValues(method, path, status).Observe(duration)
}

// ObserveGRPCDuration records a gRPC request duration.
func (m *Metrics) ObserveGRPCDuration(method, status string, duration float64) {
	m.grpcDuration.WithLabelValues(method, status).Observe(duration)
}

// ObserveRiskScore records a risk score.
func (m *Metrics) ObserveRiskScore(outcome string, score float64) {
	m.riskScore.WithLabelValues(outcome).Observe(score)
}

// SetAdapterHealth updates the adapter health gauge.
// status should be "healthy", "degraded", or "unhealthy".
func (m *Metrics) SetAdapterHealth(adapter, status string) {
	var value float64
	switch status {
	case "healthy":
		value = 1.0
	case "degraded":
		value = 0.5
	}
	switch adapter {
	case "mnemos":
		m.mnemosHealth.WithLabelValues(adapter).Set(value)
	case "chronos":
		m.chronosHealth.WithLabelValues(adapter).Set(value)
	}
}

// Handler returns an http.Handler for the /metrics endpoint.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// Snapshot returns a stable copy of key counters (for JSON endpoints).
func (m *Metrics) Snapshot() map[string]uint64 {
	return map[string]uint64{}
}

// RiskDistribution returns risk bucket counts (stub for compatibility).
func (m *Metrics) RiskDistribution() map[string]uint64 {
	return map[string]uint64{}
}

// SetGauge is a helper for gauge metrics (stub for compatibility).
func (m *Metrics) SetGauge(key string, value uint64) {
	// Stub - Prometheus uses different patterns for gauges
}

// IncGauge is a helper for gauge metrics (stub for compatibility).
func (m *Metrics) IncGauge(key string, delta int64) {
	// Stub
}

// DecGauge is a helper for gauge metrics (stub for compatibility).
func (m *Metrics) DecGauge(key string) {
	// Stub
}

var (
	metricsInstance *Metrics
	metricsOnce   sync.Once
)

// DefaultMetrics returns the global metrics instance.
func DefaultMetrics() *Metrics {
	metricsOnce.Do(func() {
		metricsInstance = NewMetrics()
	})
	return metricsInstance
}

// SLOThresholds defines the SLO targets for Nous.
type SLOThresholds struct {
	UptimePercent    float64 // target: 99.9
	P99LatencySeconds float64 // target: 0.1 (100ms)
	ErrorRatePercent  float64 // target: 0.1
}

// DefaultSLOs returns the default SLO thresholds.
func DefaultSLOs() SLOThresholds {
	return SLOThresholds{
		UptimePercent:    99.9,
		P99LatencySeconds: 0.1,
		ErrorRatePercent:  0.1,
	}
}
