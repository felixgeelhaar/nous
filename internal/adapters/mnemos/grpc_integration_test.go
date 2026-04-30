package mnemos

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/felixgeelhaar/nous/internal/circuit"

	mnemosv1 "github.com/felixgeelhaar/mnemos/proto/gen/mnemos/v1"
	"github.com/felixgeelhaar/nous/internal/ports"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// mockMnemosServer is a minimal in-memory gRPC implementation for
// adapter tests.
type mockMnemosServer struct {
	mnemosv1.UnimplementedMnemosServiceServer
	events []*mnemosv1.Event
}

func (m *mockMnemosServer) ListEvents(_ context.Context, req *mnemosv1.ListEventsRequest) (*mnemosv1.ListEventsResponse, error) {
	limit := int(req.Pagination.GetLimit())
	if limit <= 0 || limit > len(m.events) {
		limit = len(m.events)
	}
	return &mnemosv1.ListEventsResponse{
		Events: m.events[:limit],
		Total:  int32(len(m.events)),
		Limit:  int32(limit),
	}, nil
}

func (m *mockMnemosServer) AppendEvents(_ context.Context, req *mnemosv1.AppendEventsRequest) (*mnemosv1.AppendResponse, error) {
	m.events = append(m.events, req.Events...)
	return &mnemosv1.AppendResponse{Accepted: int32(len(req.Events))}, nil
}

func startMockMnemosServer(t *testing.T) (*grpc.ClientConn, func()) {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	mock := &mockMnemosServer{}
	mnemosv1.RegisterMnemosServiceServer(srv, mock)

	go func() {
		if err := srv.Serve(lis); err != nil {
			t.Logf("mock server serve: %v", err)
		}
	}()

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	return conn, func() {
		_ = conn.Close()
		srv.Stop()
		_ = lis.Close()
	}
}

func TestAdapter_Recall(t *testing.T) {
	conn, cleanup := startMockMnemosServer(t)
	defer cleanup()

	a := &Adapter{client: mnemosv1.NewMnemosServiceClient(conn), breaker: circuit.New(circuit.DefaultConfig())}
	ctx := context.Background()
	_, err := a.client.AppendEvents(ctx, &mnemosv1.AppendEventsRequest{
		Events: []*mnemosv1.Event{
			{Id: "evt-1", Content: "hello", Timestamp: timestamppb.Now()},
			{Id: "evt-2", Content: "world", Timestamp: timestamppb.Now()},
		},
	})
	require.NoError(t, err)

	mems, err := a.Recall(ctx, ports.RecallQuery{Limit: 5})
	require.NoError(t, err)
	require.Len(t, mems, 2)
	require.Equal(t, "evt-1", mems[0].ID)
	require.Equal(t, "event", mems[0].Kind)
	require.False(t, mems[0].OccurredAt.IsZero())
}

func TestAdapter_Recall_DefaultLimit(t *testing.T) {
	conn, cleanup := startMockMnemosServer(t)
	defer cleanup()

	a := &Adapter{client: mnemosv1.NewMnemosServiceClient(conn), breaker: circuit.New(circuit.DefaultConfig())}
	ctx := context.Background()

	_, err := a.client.AppendEvents(ctx, &mnemosv1.AppendEventsRequest{
		Events: []*mnemosv1.Event{
			{Id: "evt-1", Content: "a", Timestamp: timestamppb.Now()},
		},
	})
	require.NoError(t, err)

	// Limit=0 should default to 10.
	mems, err := a.Recall(ctx, ports.RecallQuery{})
	require.NoError(t, err)
	require.Len(t, mems, 1)
}

func TestAdapter_AppendEvent(t *testing.T) {
	conn, cleanup := startMockMnemosServer(t)
	defer cleanup()

	a := &Adapter{client: mnemosv1.NewMnemosServiceClient(conn), breaker: circuit.New(circuit.DefaultConfig())}
	ctx := context.Background()

	id, err := a.AppendEvent(ctx, ports.MnemosEvent{
		Kind:       "decision",
		OccurredAt: time.Now().UTC(),
		Actor:      "nous",
		Subject:    "commitment-1",
		Body: map[string]any{
			"score": 0.75,
			"note":  "evaluated",
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, id)
	require.Contains(t, id, "commitment-1")

	// Verify it was stored.
	resp, err := a.client.ListEvents(ctx, &mnemosv1.ListEventsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Events, 1)
	require.Equal(t, "decision", resp.Events[0].Content)
	require.Equal(t, "0.75", resp.Events[0].Metadata["score"])
	require.Equal(t, "evaluated", resp.Events[0].Metadata["note"])
}

func TestAdapter_AppendEvent_NonStringBody(t *testing.T) {
	conn, cleanup := startMockMnemosServer(t)
	defer cleanup()

	a := &Adapter{client: mnemosv1.NewMnemosServiceClient(conn), breaker: circuit.New(circuit.DefaultConfig())}
	ctx := context.Background()

	_, err := a.AppendEvent(ctx, ports.MnemosEvent{
		Kind:       "test",
		OccurredAt: time.Now().UTC(),
		Subject:    "s1",
		Body: map[string]any{
			"count": 42,
			"flag":  true,
		},
	})
	require.NoError(t, err)

	resp, err := a.client.ListEvents(ctx, &mnemosv1.ListEventsRequest{})
	require.NoError(t, err)
	require.Equal(t, "42", resp.Events[0].Metadata["count"])
	require.Equal(t, "true", resp.Events[0].Metadata["flag"])
}
