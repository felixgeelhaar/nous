package grpc

import (
	"context"
	"crypto/subtle"
	"strings"

	"github.com/felixgeelhaar/nous/internal/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// BearerAuthInterceptor enforces a static bearer token on every unary
// RPC. An empty token disables auth (returns a no-op interceptor).
// The token is compared in constant time to avoid timing oracles.
func BearerAuthInterceptor(token string) grpc.UnaryServerInterceptor {
	if token == "" {
		return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			return handler(ctx, req)
		}
	}
	expected := []byte("Bearer " + token)
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}
		got := firstAuthHeader(md)
		if got == "" {
			return nil, status.Error(codes.Unauthenticated, "missing authorization metadata")
		}
		if subtle.ConstantTimeCompare([]byte(got), expected) != 1 {
			return nil, status.Error(codes.Unauthenticated, "invalid bearer token")
		}
		return handler(ctx, req)
	}
}

func firstAuthHeader(md metadata.MD) string {
	for _, key := range []string{"authorization", "Authorization"} {
		if v := md.Get(key); len(v) > 0 {
			return strings.TrimSpace(v[0])
		}
	}
	return ""
}

// CombinedAuthInterceptor accepts either a static bearer token OR a
// verified JWT. Empty staticToken AND nil verifier disables auth
// (returns a no-op interceptor).
func CombinedAuthInterceptor(staticToken string, verifier *auth.Verifier) grpc.UnaryServerInterceptor {
	if staticToken == "" && verifier == nil {
		return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			return handler(ctx, req)
		}
	}
	expected := []byte("Bearer " + staticToken)
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}
		got := firstAuthHeader(md)
		if got == "" {
			return nil, status.Error(codes.Unauthenticated, "missing authorization metadata")
		}
		if staticToken != "" && subtle.ConstantTimeCompare([]byte(got), expected) == 1 {
			return handler(ctx, req)
		}
		if verifier != nil && strings.HasPrefix(got, "Bearer ") {
			if _, err := verifier.Verify(strings.TrimPrefix(got, "Bearer ")); err == nil {
				return handler(ctx, req)
			}
		}
		return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
	}
}
