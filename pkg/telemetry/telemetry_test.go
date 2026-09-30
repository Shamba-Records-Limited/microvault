package telemetry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func newRecorder(t *testing.T) (*tracetest.SpanRecorder, *sdktrace.TracerProvider) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(redactProcessor{}), sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return rec, tp
}

func attr(s sdktrace.ReadOnlySpan, key attribute.Key) (string, bool) {
	for _, kv := range s.Attributes() {
		if kv.Key == key {
			return kv.Value.AsString(), true
		}
	}
	return "", false
}

func TestRedactStripsQueryFromURLAttributes(t *testing.T) {
	rec, tp := newRecorder(t)
	_, span := tp.Tracer("t").Start(t.Context(), "call",
		trace.WithAttributes(
			attribute.String("url.full", "https://anchor.example/sep24?token=secret&x=1"),
			attribute.String("url.query", "token=secret"),
			attribute.String("server.address", "anchor.example"),
		))
	span.End()

	got := rec.Ended()[0]
	if v, _ := attr(got, "url.full"); v != "https://anchor.example/sep24" {
		t.Errorf("url.full = %q", v)
	}
	if v, _ := attr(got, "url.query"); v != "REDACTED" {
		t.Errorf("url.query = %q", v)
	}
	if v, _ := attr(got, "server.address"); v != "anchor.example" {
		t.Errorf("server.address = %q", v)
	}
}

func TestTransportDoesNotInjectTraceHeaders(t *testing.T) {
	var seen http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	_, tp := newRecorder(t)
	ctx, span := tp.Tracer("t").Start(t.Context(), "parent")
	defer span.End()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/x?token=secret", http.NoBody)
	resp, err := Client(srv.Client()).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if seen.Get("Traceparent") != "" {
		t.Fatalf("traceparent leaked to partner: %v", seen)
	}
}

func TestSetupWithoutEndpointIsNoop(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	shutdown, err := Setup(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
}
