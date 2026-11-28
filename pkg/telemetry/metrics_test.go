package telemetry

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/Shamba-Records-Limited/microvault/pkg/logging"
)

func collect(t *testing.T, r *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := r.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	return rm
}

func find(rm metricdata.ResourceMetrics, name string) (metricdata.Metrics, bool) {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				return m, true
			}
		}
	}
	return metricdata.Metrics{}, false
}

func TestServerMetricsDropHostHeaderAddress(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader), sdkmetric.WithView(boundedServerMetrics))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	attrs := metric.WithAttributes(attribute.String("server.address", "attacker-chosen.example"), attribute.Int("server.port", 31337), attribute.String("http.route", "/loans/:id"))
	h, _ := mp.Meter("github.com/gofiber/contrib/v3/otel").Float64Histogram("http.server.request.duration")
	h.Record(context.Background(), 1, attrs)
	other, _ := mp.Meter("other").Float64Histogram("http.client.duration")
	other.Record(context.Background(), 1, attrs)

	rm := collect(t, reader)
	server, _ := find(rm, "http.server.request.duration")
	for _, dp := range server.Data.(metricdata.Histogram[float64]).DataPoints {
		if _, ok := dp.Attributes.Value("server.address"); ok {
			t.Fatal("server metric kept server.address")
		}
		if _, ok := dp.Attributes.Value("server.port"); ok {
			t.Fatal("server metric kept server.port")
		}
		if _, ok := dp.Attributes.Value("http.route"); !ok {
			t.Fatal("server metric lost http.route")
		}
	}
	client, _ := find(rm, "http.client.duration")
	if _, ok := client.Data.(metricdata.Histogram[float64]).DataPoints[0].Attributes.Value("server.address"); !ok {
		t.Fatal("view touched a non-server instrument")
	}
}

// The work duration instrument is created at package init and binds to the
// first global provider installed, so the tests that read it share one.
var workReader = sync.OnceValue(func() *sdkmetric.ManualReader {
	reader := sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	otel.SetTracerProvider(sdktrace.NewTracerProvider())
	return reader
})

// workCounts sums the work duration's data points for work by outcome.
func workCounts(t *testing.T, reader *sdkmetric.ManualReader, work string) map[string]uint64 {
	t.Helper()
	counts := map[string]uint64{}
	m, ok := find(collect(t, reader), "microvault.background.work.duration")
	if !ok {
		return counts
	}
	for _, dp := range m.Data.(metricdata.Histogram[float64]).DataPoints {
		if w, _ := dp.Attributes.Value("work"); w.AsString() != work {
			continue
		}
		o, _ := dp.Attributes.Value("outcome")
		counts[o.AsString()] += dp.Count
	}
	return counts
}

func TestStartRootRecordsWorkDuration(t *testing.T) {
	reader := workReader()
	before := workCounts(t, reader, "test.work")

	_, span := StartRoot(t.Context(), "test.work")
	span.End()

	after := workCounts(t, reader, "test.work")
	if after[OutcomeOK]-before[OutcomeOK] != 1 || after[OutcomeError] != before[OutcomeError] {
		t.Fatalf("got %v after %v, want one more ok", after, before)
	}
}

// Work fails either by marking its span or, as most pollers do, by logging
// an error under its context.
func TestStartRootRecordsTheOutcome(t *testing.T) {
	reader := workReader()
	before := workCounts(t, reader, "test.outcome")

	ctx, span := StartRoot(t.Context(), "test.outcome")
	logging.New(io.Discard, slog.LevelInfo).ErrorContext(ctx, "could not settle")
	span.End()

	_, span = StartRoot(t.Context(), "test.outcome")
	RecordError(span, errors.New("x"))
	span.End()

	_, span = StartRoot(t.Context(), "test.outcome")
	span.End()

	after := workCounts(t, reader, "test.outcome")
	if after[OutcomeError]-before[OutcomeError] != 2 || after[OutcomeOK]-before[OutcomeOK] != 1 {
		t.Fatalf("got %v after %v, want 2 more error (logged, span) and 1 more ok", after, before)
	}
}
