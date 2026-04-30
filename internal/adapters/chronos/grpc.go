// Package chronos provides a gRPC adapter that wraps the Chronos
// gRPC client and implements ports.ChronosClient.
package chronos

import (
	"context"
	"fmt"
	"google.golang.org/grpc/credentials"

	chronosv1 "github.com/felixgeelhaar/chronos/api/proto/chronos/v1"
	"github.com/felixgeelhaar/nous/internal/domain"
	"github.com/felixgeelhaar/nous/internal/ports"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Adapter implements ports.ChronosClient over the Chronos gRPC API.
type Adapter struct {
	conn       *grpc.ClientConn
	client     chronosv1.ChronosServiceClient
	bearerToken string
}

// callOpts returns grpc.CallOption with bearer token if configured.
func (a *Adapter) callOpts() []grpc.CallOption {
	if a.bearerToken == "" {
		return nil
	}
	return []grpc.CallOption{grpc.PerRPCCredentials(bearerTokenCreds{a.bearerToken})}
}

// NewGRPC returns an Adapter backed by a gRPC connection to addr.
// Use empty addr to create an unconfigured adapter whose methods
// return ErrClientNotConfigured.
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
			return nil, fmt.Errorf("chronos grpc: load TLS cert %s: %w", tlsCertFile, err)
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.NewClient(addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("chronos grpc: dial %s: %w", addr, err)
	}
	adapter := &Adapter{
		conn:   conn,
		client: chronosv1.NewChronosServiceClient(conn),
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

// GetSignals queries Chronos for temporal signals matching the filter.
func (a *Adapter) GetSignals(ctx context.Context, filter ports.SignalFilter) ([]domain.ChronosSignal, error) {
	if a.client == nil {
		return nil, ports.ErrClientNotConfigured
	}
	scopeID, err := uuid.Parse(filter.ScopeID)
	if err != nil {
		return nil, fmt.Errorf("chronos grpc: invalid scope_id %q: %w", filter.ScopeID, err)
	}

	req := &chronosv1.ListSignalsRequest{
		ScopeId: scopeID.String(),
		Limit:   int32(filter.Limit),
	}
	if filter.Since != nil {
		req.Since = timestamppb.New(*filter.Since)
	}
	if len(filter.Patterns) > 0 {
		req.Pattern = patternTypeFromString(filter.Patterns[0])
	}

	resp, err := a.client.ListSignals(ctx, req, a.callOpts()...)
	if err != nil {
		return nil, fmt.Errorf("chronos grpc: list signals: %w", err)
	}

	signals := make([]domain.ChronosSignal, 0, len(resp.Signals))
	for _, s := range resp.Signals {
		signals = append(signals, domain.ChronosSignal{
			ID:         parseUUID(s.Id),
			Pattern:    patternTypeToString(s.Pattern),
			ScopeID:    parseUUID(s.ScopeId),
			Series:     parseUUID(s.Series),
			Confidence: s.Confidence,
			DetectedAt: s.DetectedAt.AsTime(),
		})
	}
	return signals, nil
}

// patternTypeFromString maps a client PatternType constant to the proto enum.
func patternTypeFromString(s string) chronosv1.PatternType {
	switch s {
	case "recurrence":
		return chronosv1.PatternType_PATTERN_TYPE_RECURRENCE
	case "trend":
		return chronosv1.PatternType_PATTERN_TYPE_TREND
	case "spike":
		return chronosv1.PatternType_PATTERN_TYPE_SPIKE
	case "drop":
		return chronosv1.PatternType_PATTERN_TYPE_DROP
	case "stall":
		return chronosv1.PatternType_PATTERN_TYPE_STALL
	case "anomaly":
		return chronosv1.PatternType_PATTERN_TYPE_ANOMALY
	case "seasonality":
		return chronosv1.PatternType_PATTERN_TYPE_SEASONALITY
	case "correlation":
		return chronosv1.PatternType_PATTERN_TYPE_CORRELATION
	default:
		return chronosv1.PatternType_PATTERN_TYPE_UNSPECIFIED
	}
}

// patternTypeToString maps a proto PatternType to the client PatternType constant.
func patternTypeToString(p chronosv1.PatternType) string {
	switch p {
	case chronosv1.PatternType_PATTERN_TYPE_RECURRENCE:
		return "recurrence"
	case chronosv1.PatternType_PATTERN_TYPE_TREND:
		return "trend"
	case chronosv1.PatternType_PATTERN_TYPE_SPIKE:
		return "spike"
	case chronosv1.PatternType_PATTERN_TYPE_DROP:
		return "drop"
	case chronosv1.PatternType_PATTERN_TYPE_STALL:
		return "stall"
	case chronosv1.PatternType_PATTERN_TYPE_ANOMALY:
		return "anomaly"
	case chronosv1.PatternType_PATTERN_TYPE_SEASONALITY:
		return "seasonality"
	case chronosv1.PatternType_PATTERN_TYPE_CORRELATION:
		return "correlation"
	default:
		return ""
	}
}

// parseUUID safely parses a UUID string; returns uuid.Nil on error.
func parseUUID(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
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
