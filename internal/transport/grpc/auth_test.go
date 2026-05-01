package grpc

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestBearerAuthInterceptor_DisabledWhenEmpty(t *testing.T) {
	t.Parallel()
	interceptor := BearerAuthInterceptor("")
	called := false
	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, func(ctx context.Context, req any) (any, error) {
		called = true
		return nil, nil
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !called {
		t.Fatal("handler not called when token empty")
	}
}

func TestBearerAuthInterceptor_RejectsMissingMetadata(t *testing.T) {
	t.Parallel()
	interceptor := BearerAuthInterceptor("secret")
	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, func(ctx context.Context, req any) (any, error) {
		t.Fatal("handler called")
		return nil, nil
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("code = %v", status.Code(err))
	}
}

func TestBearerAuthInterceptor_RejectsWrongToken(t *testing.T) {
	t.Parallel()
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer wrong"))
	interceptor := BearerAuthInterceptor("secret")
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, func(ctx context.Context, req any) (any, error) {
		t.Fatal("handler called")
		return nil, nil
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("code = %v", status.Code(err))
	}
}

func TestBearerAuthInterceptor_AcceptsValidToken(t *testing.T) {
	t.Parallel()
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer secret"))
	interceptor := BearerAuthInterceptor("secret")
	called := false
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, func(ctx context.Context, req any) (any, error) {
		called = true
		return nil, nil
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !called {
		t.Fatal("handler not called for valid token")
	}
}
