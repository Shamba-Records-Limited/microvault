package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
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

	attrs := metric.WithAttributes(attribute.String("server.address", "attacker-chosen.example"), attribute.String("http.route", "/loans/:id"))
	h, _ := mp.Meter("github.com/gofiber/contrib/otelfiber").Float64Histogram("http.server.duration")
	h.Record(context.Background(), 1, attrs)
	other, _ := mp.Meter("other").Float64Histogram("http.client.duration")
	other.Record(context.Background(), 1, attrs)

	rm := collect(t, reader)
	server, _ := find(rm, "http.server.duration")
	for _, dp := range server.Data.(metricdata.Histogram[float64]).DataPoints {
		if _, ok := dp.Attributes.Value("server.address"); ok {
			t.Fatal("server metric kept server.address")
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

func TestStartRootRecordsWorkDuration(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	_, span := StartRoot(t.Context(), "test.work")
	span.End()

	m, ok := find(collect(t, reader), "microvault.background.work.duration")
	if !ok {
		t.Fatal("work duration not recorded")
	}
	dp := m.Data.(metricdata.Histogram[float64]).DataPoints[0]
	if v, _ := dp.Attributes.Value("work"); v.AsString() != "test.work" || dp.Count != 1 {
		t.Fatalf("got work=%v count=%d", v.AsString(), dp.Count)
	}
}
