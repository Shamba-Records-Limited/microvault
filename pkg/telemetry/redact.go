package telemetry

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

var urlKeys = map[attribute.Key]struct{}{
	"url.full":    {},
	"url.query":   {},
	"http.url":    {},
	"http.target": {},
}

// redactProcessor drops query strings from URL attributes when a span starts,
// before any exporter sees them.
type redactProcessor struct{}

func (redactProcessor) OnStart(_ context.Context, s sdktrace.ReadWriteSpan) {
	var fixed []attribute.KeyValue
	for _, kv := range s.Attributes() {
		if _, ok := urlKeys[kv.Key]; !ok || kv.Value.Type() != attribute.STRING {
			continue
		}
		v := kv.Value.AsString()
		if kv.Key == "url.query" {
			fixed = append(fixed, attribute.String(string(kv.Key), "REDACTED"))
			continue
		}
		if i := strings.IndexByte(v, '?'); i >= 0 {
			fixed = append(fixed, attribute.String(string(kv.Key), v[:i]))
		}
	}
	if len(fixed) > 0 {
		s.SetAttributes(fixed...)
	}
}

func (redactProcessor) OnEnd(sdktrace.ReadOnlySpan)      {}
func (redactProcessor) Shutdown(context.Context) error   { return nil }
func (redactProcessor) ForceFlush(context.Context) error { return nil }
