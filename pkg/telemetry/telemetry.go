package telemetry

import (
	"context"
	"errors"
	"os"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// ScopeName is the instrumentation scope for spans and metrics the platform
// creates itself.
const ScopeName = "github.com/Shamba-Records-Limited/microvault"

// Enabled reports whether an OTLP endpoint is configured for traces or
// metrics.
func Enabled() bool {
	for _, k := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"} {
		if os.Getenv(k) != "" {
			return true
		}
	}
	return false
}

// Setup installs the W3C propagators and, when an endpoint is configured, a
// batching tracer provider and a periodic meter provider, both exporting over
// OTLP gRPC, plus Go runtime metrics. The returned shutdown flushes pending
// spans and metrics; it is a no-op when telemetry is disabled.
func Setup(ctx context.Context) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	if !Enabled() {
		return func(context.Context) error { return nil }, nil
	}

	res, err := resource.Merge(resource.Default(), resource.Environment())
	if err != nil && !errors.Is(err, resource.ErrPartialResource) && !errors.Is(err, resource.ErrSchemaURLConflict) {
		return nil, err
	}

	traceExporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(redactProcessor{}),
		sdktrace.WithBatcher(traceExporter),
	)

	metricExporter, err := otlpmetricgrpc.New(ctx)
	if err != nil {
		return nil, errors.Join(err, tp.Shutdown(ctx))
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
		sdkmetric.WithView(boundedServerMetrics),
	)
	if err := runtime.Start(runtime.WithMeterProvider(mp)); err != nil {
		return nil, errors.Join(err, tp.Shutdown(ctx), mp.Shutdown(ctx))
	}

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	return func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx))
	}, nil
}

// boundedServerMetrics drops server.address from the HTTP server metrics:
// otelfiber takes it from the Host header, which the caller controls, so it
// would let any client mint new series.
func boundedServerMetrics(inst sdkmetric.Instrument) (sdkmetric.Stream, bool) {
	if inst.Scope.Name != "github.com/gofiber/contrib/otelfiber" {
		return sdkmetric.Stream{}, false
	}
	return sdkmetric.Stream{
		Name:            inst.Name,
		Description:     inst.Description,
		Unit:            inst.Unit,
		AttributeFilter: attribute.NewDenyKeysFilter("server.address"),
	}, true
}

// Tracer returns the platform's tracer from the global provider.
func Tracer() trace.Tracer {
	return otel.Tracer(ScopeName)
}

// Meter returns the platform's meter from the global provider. Instruments
// created before Setup bind to the real provider once it is installed.
func Meter() metric.Meter {
	return otel.Meter(ScopeName)
}

var workDuration, _ = Meter().Float64Histogram("microvault.background.work.duration",
	metric.WithUnit("s"),
	metric.WithDescription("Duration of one unit of background work — a poller item or a sweep — labelled by work name."))

// StartRoot starts a new root span for a unit of background work — one
// poller item, one sweep — so each gets its own trace instead of joining
// whatever the caller's context carried. Ending the returned span also
// records the work's duration, labelled by name, so name must come from a
// fixed set.
func StartRoot(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	ctx, span := Tracer().Start(ctx, name, trace.WithNewRoot(), trace.WithSpanKind(trace.SpanKindInternal), trace.WithAttributes(attrs...))
	return ctx, &workSpan{Span: span, ctx: ctx, name: name, start: time.Now()}
}

type workSpan struct {
	trace.Span
	ctx   context.Context
	name  string
	start time.Time
}

func (w *workSpan) End(opts ...trace.SpanEndOption) {
	workDuration.Record(w.ctx, time.Since(w.start).Seconds(), metric.WithAttributes(attribute.String("work", w.name)))
	w.Span.End(opts...)
}

// RecordError marks span as failed with err. A nil err is a no-op.
func RecordError(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// Outcome is "error" for a non-nil err and "ok" otherwise — the bounded
// outcome label every platform metric uses.
func Outcome(err error) string {
	if err != nil {
		return "error"
	}
	return "ok"
}
