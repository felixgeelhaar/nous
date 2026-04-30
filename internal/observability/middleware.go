package observability

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// HTTPMiddleware wraps an http.Handler with correlation-ID injection
// and OpenTelemetry tracing.
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Correlation-ID")
		if id == "" {
			id = r.Header.Get("X-Request-ID")
		}
		if id == "" {
			// Fallback to a short timestamp-based ID; not UUID to avoid
			// heavy imports in middleware.
			id = formatShortID()
		}

		ctx := WithCorrelationID(r.Context(), id)

		if Tracer != nil {
			ctx, span := Tracer.Start(ctx, r.Method+" "+r.URL.Path,
				trace.WithAttributes(
					attribute.String("http.method", r.Method),
					attribute.String("http.path", r.URL.Path),
					attribute.String("correlation_id", id),
				),
			)
			defer span.End()
			w.Header().Set("X-Correlation-ID", id)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		w.Header().Set("X-Correlation-ID", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// formatShortID returns a nanosecond timestamp as a compact string.
func formatShortID() string {
	return time.Now().UTC().Format("20060102T150405.000000000")
}

// GRPCUnaryInterceptor extracts correlation IDs from incoming gRPC
// metadata and injects them into the context.
func GRPCUnaryInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("x-correlation-id"); len(vals) > 0 && vals[0] != "" {
			ctx = WithCorrelationID(ctx, vals[0])
		}
	}

	if Tracer != nil {
		ctx, span := Tracer.Start(ctx, info.FullMethod,
			trace.WithAttributes(attribute.String("rpc.method", info.FullMethod)),
		)
		defer span.End()
		return handler(ctx, req)
	}

	return handler(ctx, req)
}
