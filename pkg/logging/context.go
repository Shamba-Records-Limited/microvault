package logging

import (
	"context"
	"log/slog"
)

type attrsKey struct{}

// With returns a child context carrying attrs in addition to any already
// attached to ctx.
func With(ctx context.Context, attrs ...slog.Attr) context.Context {
	if len(attrs) == 0 {
		return ctx
	}
	prev := Attrs(ctx)
	merged := make([]slog.Attr, 0, len(prev)+len(attrs))
	merged = append(merged, prev...)
	merged = append(merged, attrs...)
	return context.WithValue(ctx, attrsKey{}, merged)
}

// Attrs returns the attributes attached to ctx by With.
func Attrs(ctx context.Context) []slog.Attr {
	if ctx == nil {
		return nil
	}
	attrs, _ := ctx.Value(attrsKey{}).([]slog.Attr)
	return attrs
}
