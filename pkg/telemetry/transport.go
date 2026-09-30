package telemetry

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/propagation"
)

// Transport wraps base so each request is a client span named by method and
// host. A nil base means http.DefaultTransport. Trace headers are not
// injected.
func Transport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return otelhttp.NewTransport(base,
		otelhttp.WithPropagators(propagation.NewCompositeTextMapPropagator()),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Host
		}),
	)
}

// Client returns a copy of c whose transport is wrapped with Transport. A nil
// c yields a client with only the wrapped default transport.
func Client(c *http.Client) *http.Client {
	if c == nil {
		return &http.Client{Transport: Transport(nil)}
	}
	wrapped := *c
	wrapped.Transport = Transport(c.Transport)
	return &wrapped
}
