// Package mnemos provides a gRPC adapter that wraps the Mnemos service
// client and implements ports.MnemosClient.
//
// The adapter translates Nous domain queries into Mnemos gRPC calls.
// Recall is best-effort: Mnemos gRPC does not yet expose semantic
// search, so it falls back to listing the most recent events.
package mnemos

import (
	"context"
	"fmt"
	"time"

	"github.com/felixgeelhaar/mnemos/proto/gen/mnemos/v1"
	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/ports"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Adapter implements ports.MnemosClient over Mnemos gRPC.
type Adapter struct {
	client     mnemosv1.MnemosServiceClient
	conn       *grpc.ClientConn
	bearerToken string
}

// callOpts returns grpc.CallOption with bearer token if configured.
func (a *Adapter) callOpts() []grpc.CallOption {
	if a.bearerToken == "" {
		return nil
	}
	return []grpc.CallOption{grpc.PerRPCCredentials(bearerTokenCreds{a.bearerToken})}
}

// NewGRPC dials addr and returns an Adapter. Use empty addr to create
// an unconfigured adapter whose methods return ErrNotConfigured.
// If tlsCertFile is non-empty, TLS credentials are used.
// If bearerToken is non-empty, it is attached to each request via gRPC metadata.
func NewGRPC(addr, tlsCertFile, bearerToken string) (*Adapter, error) {
	if addr == "" {
		return &Adapter{}, nil
	}

	opts := []grpc.DialOption{}
	if tlsCertFile != "" {
		creds, err := credentials.NewClientTLSFromFile(tlsCertFile, "")
		if err != nil {
			return nil, fmt.Errorf("mnemos grpc: load TLS cert %s: %w", tlsCertFile, err)
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.NewClient(addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("mnemos grpc: dial %s: %w", addr, err)
	}
	adapter := &Adapter{
		client: mnemosv1.NewMnemosServiceClient(conn),
		conn:   conn,
	}
	if bearerToken != "" {
		adapter.bearerToken = bearerToken
	}
	return adapter, nil
}

// Close tears down the underlying gRPC connection.
func (a *Adapter) Close() error {
	if a.conn != nil {
		return a.conn.Close()
	}
	return nil
}

// Recall returns memories relevant to the query.
//
// Because Mnemos gRPC does not yet expose semantic search, this
// implementation falls back to ListEvents ordered by recency. The
// text and entity filters from the query are ignored. Callers should
// treat the result as "recent context" rather than "precisely
// relevant memories".
func (a *Adapter) Recall(ctx context.Context, q ports.RecallQuery) ([]domain.MemoryRef, error) {
	if a.client == nil {
		return nil, ports.ErrClientNotConfigured
	}
	limit := int32(q.Limit)
	if limit <= 0 {
		limit = 10
	}
	resp, err := a.client.ListEvents(ctx, &mnemosv1.ListEventsRequest{
		Pagination: &mnemosv1.Pagination{Limit: limit},
	}, a.callOpts()...)
	if err != nil {
		return nil, fmt.Errorf("mnemos grpc: ListEvents: %w", err)
	}
	refs := make([]domain.MemoryRef, 0, len(resp.Events))
	for _, evt := range resp.Events {
		var occurredAt time.Time
		if evt.Timestamp != nil {
			occurredAt = evt.Timestamp.AsTime()
		}
		refs = append(refs, domain.MemoryRef{
			ID:         evt.Id,
			Kind:       "event",
			OccurredAt: occurredAt,
		})
	}
	return refs, nil
}

// AppendEvent records a single event in Mnemos.
func (a *Adapter) AppendEvent(ctx context.Context, evt ports.MnemosEvent) (string, error) {
	if a.client == nil {
		return "", ports.ErrClientNotConfigured
	}
	pbEvt := &mnemosv1.Event{
		Id:            evt.Subject + "_" + time.Now().UTC().Format(time.RFC3339Nano),
		Content:       evt.Kind,
		SourceInputId: evt.Subject,
		Timestamp:     timestamppb.New(evt.OccurredAt),
		Metadata:      make(map[string]string, len(evt.Body)),
	}
	for k, v := range evt.Body {
		if s, ok := v.(string); ok {
			pbEvt.Metadata[k] = s
		} else {
			pbEvt.Metadata[k] = fmt.Sprintf("%v", v)
		}
	}
	resp, err := a.client.AppendEvents(ctx, &mnemosv1.AppendEventsRequest{
		Events: []*mnemosv1.Event{pbEvt},
	}, a.callOpts()...)
	if err != nil {
		return "", fmt.Errorf("mnemos grpc: AppendEvents: %w", err)
	}
	if resp.Accepted == 0 {
		return "", fmt.Errorf("mnemos grpc: event not accepted")
	}
	return pbEvt.Id, nil
}

// bearerTokenCreds implements grpc.PerRPCCredentials for bearer token auth.
type bearerTokenCreds struct {
	token string
}

func (b bearerTokenCreds) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	return map[string]string{
		"authorization": "Bearer " + b.token,
	}, nil
}

func (b bearerTokenCreds) RequireTransportSecurity() bool {
	return true
}
