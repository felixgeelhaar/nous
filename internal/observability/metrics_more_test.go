package observability

import (
	"net/http/httptest"
	"testing"
)

func TestMetrics_ObserveHTTPDuration(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	m.ObserveHTTPDuration("GET", "/v1/commitments", "200", 0.05)
	m.ObserveHTTPDuration("POST", "/v1/extract", "500", 1.2)
}

func TestMetrics_ObserveGRPCDuration(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	m.ObserveGRPCDuration("/nous.v1.Nous/Extract", "OK", 0.1)
	m.ObserveGRPCDuration("/nous.v1.Nous/Evaluate", "Internal", 1.5)
}

func TestMetrics_SetAdapterHealth(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	for _, status := range []string{"healthy", "degraded", "unhealthy"} {
		m.SetAdapterHealth("mnemos", status)
		m.SetAdapterHealth("chronos", status)
	}
	// Unknown adapter is a no-op (no panic).
	m.SetAdapterHealth("unknown", "healthy")
}

func TestMetrics_Handler(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	m.IncCommitmentsExtracted("success", 1)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	m.Handler().ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Errorf("status = %d", rr.Code)
	}
}

func TestMetrics_GaugeStubs(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	m.SetGauge("k", 1)
	m.IncGauge("k", 1)
	m.DecGauge("k")
}

func TestMetrics_SnapshotAndRiskDistribution(t *testing.T) {
	t.Parallel()
	m := NewMetrics()
	if m.Snapshot() == nil {
		t.Error("snapshot nil")
	}
	if m.RiskDistribution() == nil {
		t.Error("risk dist nil")
	}
}

func TestDefaultMetricsAndSLOs(t *testing.T) {
	t.Parallel()
	if m := DefaultMetrics(); m == nil {
		t.Fatal("default metrics nil")
	}
	// Idempotent on second call.
	if DefaultMetrics() == nil {
		t.Fatal("second default nil")
	}
	slos := DefaultSLOs()
	if slos.UptimePercent != 99.9 {
		t.Errorf("uptime = %v", slos.UptimePercent)
	}
	if slos.P99LatencySeconds != 0.1 {
		t.Errorf("p99 = %v", slos.P99LatencySeconds)
	}
	if slos.ErrorRatePercent != 0.1 {
		t.Errorf("error rate = %v", slos.ErrorRatePercent)
	}
}
