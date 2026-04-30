package chronos

import (
	"context"
	"net"
	"testing"
	"time"

	chronosv1 "github.com/felixgeelhaar/chronos/api/proto/chronos/v1"
	"github.com/felixgeelhaar/nous/internal/ports"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// mockChronosServer implements chronosv1.ChronosServiceServer for testing.
type mockChronosServer struct {
	chronosv1.UnimplementedChronosServiceServer
	ListSignalsFunc func(ctx context.Context, req *chronosv1.ListSignalsRequest) (*chronosv1.ListSignalsResponse, error)
}

func (m *mockChronosServer) ListSignals(ctx context.Context, req *chronosv1.ListSignalsRequest) (*chronosv1.ListSignalsResponse, error) {
	if m.ListSignalsFunc != nil {
		return m.ListSignalsFunc(ctx, req)
	}
	return &chronosv1.ListSignalsResponse{Signals: []*chronosv1.Signal{}}, nil
}

// setupTestServer creates an in-memory gRPC server with bufconn.
func setupTestServer(t *testing.T, srv chronosv1.ChronosServiceServer) (ports.ChronosClient, *bufconn.Listener) {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	chronosv1.RegisterChronosServiceServer(grpcServer, srv)
	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			t.Logf("test server stopped: %v", err)
		}
	}()
	t.Cleanup(func() { grpcServer.GracefulStop() })

	conn, err := grpc.NewClient("passthrough:///test",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to dial test server: %v", err)
	}
	client := chronosv1.NewChronosServiceClient(conn)
	return &Adapter{conn: conn, client: client}, listener
}

func TestNewGRPC_EmptyAddr_ReturnsUnconfigured(t *testing.T) {
	adapter, err := NewGRPC("", "", "")
	if err != nil {
		t.Fatalf("NewGRPC('', '', '') error: %v", err)
	}
	if adapter.client != nil {
		t.Fatal("expected nil client for empty addr")
	}
}

func TestGetSignals_Unconfigured_ReturnsErrClientNotConfigured(t *testing.T) {
	adapter, _ := NewGRPC("", "", "")
	_, err := adapter.GetSignals(context.Background(), ports.SignalFilter{ScopeID: uuid.New().String()})
	if err != ports.ErrClientNotConfigured {
		t.Fatalf("expected ErrClientNotConfigured, got: %v", err)
	}
}

func TestGetSignals_ValidRequest_ReturnsSignals(t *testing.T) {
	scopeID := uuid.New()
	seriesID := uuid.New()
	now := time.Now()

	mockServer := &mockChronosServer{
		ListSignalsFunc: func(ctx context.Context, req *chronosv1.ListSignalsRequest) (*chronosv1.ListSignalsResponse, error) {
			if req.ScopeId != scopeID.String() {
				t.Errorf("expected scope_id %s, got %s", scopeID, req.ScopeId)
			}
			if req.Limit != 10 {
				t.Errorf("expected limit 10, got %d", req.Limit)
			}
			return &chronosv1.ListSignalsResponse{
				Signals: []*chronosv1.Signal{
					{
						Id:         uuid.New().String(),
						ScopeId:    scopeID.String(),
						Series:     seriesID.String(),
						Pattern:    chronosv1.PatternType_PATTERN_TYPE_TREND,
						Confidence:  0.95,
						DetectedAt: timestamppb.New(now),
					},
				},
			}, nil
		},
	}

	adapter, _ := setupTestServer(t, mockServer)
	defer func() { _ = adapter.(*Adapter).Close() }()

	since := now.Add(-24 * time.Hour)
	signals, err := adapter.GetSignals(context.Background(), ports.SignalFilter{
		ScopeID: scopeID.String(),
		Limit:   10,
		Since:   &since,
		Patterns: []string{"trend"},
	})
	if err != nil {
		t.Fatalf("GetSignals error: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}
	if signals[0].Pattern != "trend" {
		t.Errorf("expected pattern 'trend', got %q", signals[0].Pattern)
	}
	if signals[0].Confidence != 0.95 {
		t.Errorf("expected confidence 0.95, got %f", signals[0].Confidence)
	}
	if signals[0].ScopeID != scopeID {
		t.Errorf("expected scope_id %v, got %v", scopeID, signals[0].ScopeID)
	}
}

func TestGetSignals_EmptyResponse_ReturnsEmptySlice(t *testing.T) {
	mockServer := &mockChronosServer{
		ListSignalsFunc: func(ctx context.Context, req *chronosv1.ListSignalsRequest) (*chronosv1.ListSignalsResponse, error) {
			return &chronosv1.ListSignalsResponse{Signals: []*chronosv1.Signal{}}, nil
		},
	}

	adapter, _ := setupTestServer(t, mockServer)
	defer func() { _ = adapter.(*Adapter).Close() }()

	signals, err := adapter.GetSignals(context.Background(), ports.SignalFilter{
		ScopeID: uuid.New().String(),
	})
	if err != nil {
		t.Fatalf("GetSignals error: %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("expected 0 signals, got %d", len(signals))
	}
}

func TestGetSignals_gRPCError_ReturnsWrappedError(t *testing.T) {
	mockServer := &mockChronosServer{
		ListSignalsFunc: func(ctx context.Context, req *chronosv1.ListSignalsRequest) (*chronosv1.ListSignalsResponse, error) {
			return nil, status.Error(codes.Internal, "simulated failure")
		},
	}

	adapter, _ := setupTestServer(t, mockServer)
	defer func() { _ = adapter.(*Adapter).Close() }()

	_, err := adapter.GetSignals(context.Background(), ports.SignalFilter{
		ScopeID: uuid.New().String(),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	expectedMsg := "chronos grpc: list signals"
	if err != nil && !contains(err.Error(), expectedMsg) {
		t.Errorf("expected error containing %q, got %q", expectedMsg, err.Error())
	}
}

func TestGetSignals_InvalidScopeID_ReturnsError(t *testing.T) {
	adapter, _ := NewGRPC("passthrough:///test", "", "")
	_, err := adapter.GetSignals(context.Background(), ports.SignalFilter{
		ScopeID: "not-a-uuid",
	})
	if err == nil {
		t.Fatal("expected error for invalid scope_id, got nil")
	}
}

func TestGetSignals_AllPatternTypes_MappedCorrectly(t *testing.T) {
	patternTests := []struct {
		input    string
		expected chronosv1.PatternType
	}{
		{"recurrence", chronosv1.PatternType_PATTERN_TYPE_RECURRENCE},
		{"trend", chronosv1.PatternType_PATTERN_TYPE_TREND},
		{"spike", chronosv1.PatternType_PATTERN_TYPE_SPIKE},
		{"drop", chronosv1.PatternType_PATTERN_TYPE_DROP},
		{"stall", chronosv1.PatternType_PATTERN_TYPE_STALL},
		{"anomaly", chronosv1.PatternType_PATTERN_TYPE_ANOMALY},
		{"seasonality", chronosv1.PatternType_PATTERN_TYPE_SEASONALITY},
		{"correlation", chronosv1.PatternType_PATTERN_TYPE_CORRELATION},
		{"", chronosv1.PatternType_PATTERN_TYPE_UNSPECIFIED},
	}

	for _, tt := range patternTests {
		result := patternTypeFromString(tt.input)
		if result != tt.expected {
			t.Errorf("patternTypeFromString(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestPatternTypeToString_AllTypes_MappedCorrectly(t *testing.T) {
	typeTests := []struct {
		input    chronosv1.PatternType
		expected string
	}{
		{chronosv1.PatternType_PATTERN_TYPE_RECURRENCE, "recurrence"},
		{chronosv1.PatternType_PATTERN_TYPE_TREND, "trend"},
		{chronosv1.PatternType_PATTERN_TYPE_SPIKE, "spike"},
		{chronosv1.PatternType_PATTERN_TYPE_DROP, "drop"},
		{chronosv1.PatternType_PATTERN_TYPE_STALL, "stall"},
		{chronosv1.PatternType_PATTERN_TYPE_ANOMALY, "anomaly"},
		{chronosv1.PatternType_PATTERN_TYPE_SEASONALITY, "seasonality"},
		{chronosv1.PatternType_PATTERN_TYPE_CORRELATION, "correlation"},
		{chronosv1.PatternType_PATTERN_TYPE_UNSPECIFIED, ""},
	}

	for _, tt := range typeTests {
		result := patternTypeToString(tt.input)
		if result != tt.expected {
			t.Errorf("patternTypeToString(%v) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestGetSignals_WithoutSince_RequestHasNilSince(t *testing.T) {
	var receivedReq *chronosv1.ListSignalsRequest
	mockServer := &mockChronosServer{
		ListSignalsFunc: func(ctx context.Context, req *chronosv1.ListSignalsRequest) (*chronosv1.ListSignalsResponse, error) {
			receivedReq = req
			return &chronosv1.ListSignalsResponse{Signals: []*chronosv1.Signal{}}, nil
		},
	}

	adapter, _ := setupTestServer(t, mockServer)
	defer func() { _ = adapter.(*Adapter).Close() }()

	_, _ = adapter.GetSignals(context.Background(), ports.SignalFilter{
		ScopeID: uuid.New().String(),
		Limit:   5,
	})
	if receivedReq != nil && receivedReq.Since != nil {
		t.Error("expected Since to be nil when not provided")
	}
}

func TestGetSignals_MultipleSignals_ReturnsAll(t *testing.T) {
	signalCount := 3
	signals := make([]*chronosv1.Signal, 0, signalCount)
	for i := 0; i < signalCount; i++ {
		signals = append(signals, &chronosv1.Signal{
			Id:      uuid.New().String(),
			ScopeId: uuid.New().String(),
			Pattern: chronosv1.PatternType_PATTERN_TYPE_SPIKE,
		})
	}

	mockServer := &mockChronosServer{
		ListSignalsFunc: func(ctx context.Context, req *chronosv1.ListSignalsRequest) (*chronosv1.ListSignalsResponse, error) {
			return &chronosv1.ListSignalsResponse{Signals: signals}, nil
		},
	}

	adapter, _ := setupTestServer(t, mockServer)
	defer func() { _ = adapter.(*Adapter).Close() }()

	result, err := adapter.GetSignals(context.Background(), ports.SignalFilter{
		ScopeID: uuid.New().String(),
	})
	if err != nil {
		t.Fatalf("GetSignals error: %v", err)
	}
	if len(result) != signalCount {
		t.Fatalf("expected %d signals, got %d", signalCount, len(result))
	}
}

func TestAdapter_Close_NilConn_NoError(t *testing.T) {
	adapter := &Adapter{}
	if err := adapter.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
}

func TestAdapter_Close_WithConn_ClosesCleanly(t *testing.T) {
	mockServer := &mockChronosServer{}
	adapter, _ := setupTestServer(t, mockServer)
	if err := adapter.(*Adapter).Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
}

// contains checks if substr is in s.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && (s[:len(substr)] == substr || contains(s[1:], substr)))
}
