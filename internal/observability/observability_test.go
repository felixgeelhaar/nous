package observability

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCorrelationID_ContextRoundTrip(t *testing.T) {
	ctx := WithCorrelationID(context.Background(), "abc-123")
	require.Equal(t, "abc-123", CorrelationID(ctx))
}

func TestCorrelationID_FallsBackToEmpty(t *testing.T) {
	require.Equal(t, "", CorrelationID(context.Background()))
}

func TestHandler_InjectsCorrelationID(t *testing.T) {
	var buf bytes.Buffer
	h := NewHandler(slog.NewJSONHandler(&buf, nil))
	logger := slog.New(h)

	ctx := WithCorrelationID(context.Background(), "req-42")
	logger.InfoContext(ctx, "hello")

	require.Contains(t, buf.String(), "req-42")
	require.Contains(t, buf.String(), "correlation_id")
}

func TestHandler_NoIDWhenMissing(t *testing.T) {
	var buf bytes.Buffer
	h := NewHandler(slog.NewJSONHandler(&buf, nil))
	logger := slog.New(h)

	logger.InfoContext(context.Background(), "hello")
	// correlation_id should NOT appear when not in context
	require.NotContains(t, buf.String(), "correlation_id")
}

func TestMetrics_Snapshot(t *testing.T) {
	m := NewMetrics()
	m.IncCommitmentsExtracted(3)
	m.IncEvaluationsRun(5)
	m.IncInterventionsCreated(2)

	snap := m.Snapshot()
	require.Equal(t, uint64(3), snap["commitments_extracted"])
	require.Equal(t, uint64(5), snap["evaluations_run"])
	require.Equal(t, uint64(2), snap["interventions_created"])
}

func TestMetrics_RiskDistribution(t *testing.T) {
	m := NewMetrics()
	m.RecordRiskBucket("high")
	m.RecordRiskBucket("high")
	m.RecordRiskBucket("low")

	dist := m.RiskDistribution()
	require.Equal(t, uint64(2), dist["high"])
	require.Equal(t, uint64(1), dist["low"])
}

func TestHTTPMiddleware_SetsCorrelationID(t *testing.T) {
	handler := HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NotEmpty(t, rr.Header().Get("X-Correlation-ID"))
}

func TestHTTPMiddleware_PreservesIncomingID(t *testing.T) {
	handler := HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Correlation-ID", "preserved-99")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, "preserved-99", rr.Header().Get("X-Correlation-ID"))
}

func TestHTTPMiddleware_InjectsIntoContext(t *testing.T) {
	var captured string
	handler := HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = CorrelationID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Correlation-ID", "ctx-id")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, "ctx-id", captured)
}

func TestInitTracer(t *testing.T) {
	var buf bytes.Buffer
	tp, err := InitTracer(context.Background(), &buf, "nous-test")
	require.NoError(t, err)
	defer tp.Shutdown(context.Background())

	require.NotNil(t, Tracer)

	ctx, span := Tracer.Start(context.Background(), "test-span")
	span.End()

	// Force flush so the span is written to the buffer.
	tp.ForceFlush(ctx)
	require.Eventually(t, func() bool {
		return strings.Contains(buf.String(), "test-span")
	}, time.Second, 10*time.Millisecond)
}
