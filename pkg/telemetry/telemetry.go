package telemetry

import (
	"context"
	"errors"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// ScopeName is the instrumentation scope for spans the platform creates
// itself.
const ScopeName = "github.com/Shamba-Records-Limited/microvault"

// Enabled reports whether an OTLP trace endpoint is configured.
func Enabled() bool {
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" || os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != ""
}

// Setup installs the W3C propagators and, when an endpoint is configured, a
// batching tracer provider exporting over OTLP gRPC. The returned shutdown
// flushes pending spans; it is a no-op when tracing is disabled.
func Setup(ctx context.Context) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	if !Enabled() {
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, err
	}
	res, err := resource.Merge(resource.Default(), resource.Environment())
	if err != nil && !errors.Is(err, resource.ErrPartialResource) && !errors.Is(err, resource.ErrSchemaURLConflict) {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(redactProcessor{}),
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

// Tracer returns the platform's tracer from the global provider.
func Tracer() trace.Tracer {
	return otel.Tracer(ScopeName)
}

// StartRoot starts a new root span for a unit of background work — one
// poller item, one sweep — so each gets its own trace instead of joining
// whatever the caller's context carried.
func StartRoot(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return Tracer().Start(ctx, name, trace.WithNewRoot(), trace.WithSpanKind(trace.SpanKindInternal), trace.WithAttributes(attrs...))
}

// RecordError marks span as failed with err. A nil err is a no-op.
func RecordError(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
