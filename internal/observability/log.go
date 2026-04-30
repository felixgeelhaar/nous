// Package observability provides cross-cutting concerns: structured
// logging with correlation IDs, OpenTelemetry tracing, and
// Prometheus-style metrics.
package observability

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// correlationKey is the context key for request correlation IDs.
type correlationKey struct{}

// WithCorrelationID injects a correlation ID into the context.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationKey{}, id)
}

// CorrelationID extracts the correlation ID from the context. If none
// is present it returns the trace span ID when tracing is active,
// otherwise an empty string.
func CorrelationID(ctx context.Context) string {
	if id, ok := ctx.Value(correlationKey{}).(string); ok && id != "" {
		return id
	}
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		return span.SpanContext().SpanID().String()
	}
	return ""
}

// Handler is a slog.Handler that injects the correlation_id attribute
// into every record.
type Handler struct {
	parent slog.Handler
}

// NewHandler wraps h so that every log record carries the correlation
// ID extracted from the context.
func NewHandler(h slog.Handler) *Handler {
	return &Handler{parent: h}
}

func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.parent.Enabled(ctx, level)
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if id := CorrelationID(ctx); id != "" {
		r.AddAttrs(slog.String("correlation_id", id))
	}
	return h.parent.Handle(ctx, r)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return NewHandler(h.parent.WithAttrs(attrs))
}

func (h *Handler) WithGroup(name string) slog.Handler {
	return NewHandler(h.parent.WithGroup(name))
}
