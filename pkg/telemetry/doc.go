// Package telemetry installs OpenTelemetry tracing for a process and provides
// the instrumentation helpers the platform's clients share.
//
// Setup reads the standard OTEL_* environment variables. With no OTLP endpoint
// configured it installs nothing, so a deployment without a collector pays
// only for non-recording spans. Spans are exported over OTLP gRPC; a span
// processor strips query strings from URL attributes before export, because
// partner URLs carry tokens there.
//
// Transport wraps an outbound http.RoundTripper so each partner call is a
// client span. It does not inject trace headers: trace context stays inside
// the platform rather than leaking to third-party APIs.
package telemetry
