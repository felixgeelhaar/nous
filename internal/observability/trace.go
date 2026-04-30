package observability

import (
	"context"
	"io"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// Tracer is the application-wide tracer. Callers should use
// observability.Tracer.Start(ctx, "span-name") rather than the raw
// OTel API so we can swap the provider in tests.
var Tracer trace.Tracer = otel.GetTracerProvider().Tracer("nous")

// InitTracer configures a tracer provider that exports spans to w
// (stdout in production, io.Discard in tests). The returned
// TracerProvider must be shut down on application exit.
func InitTracer(ctx context.Context, w io.Writer, serviceName string) (*sdktrace.TracerProvider, error) {
	exporter, err := stdouttrace.New(
		stdouttrace.WithWriter(w),
		stdouttrace.WithPrettyPrint(),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(serviceName),
		)),
	)

	otel.SetTracerProvider(tp)
	Tracer = tp.Tracer(serviceName)
	return tp, nil
}
