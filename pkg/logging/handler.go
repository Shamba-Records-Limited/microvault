package logging

import (
	"context"
	"log/slog"
)

// Handler wraps a slog.Handler and appends the attributes attached to the
// record's context with With.
type Handler struct {
	inner slog.Handler
}

// NewHandler wraps inner.
func NewHandler(inner slog.Handler) *Handler {
	return &Handler{inner: inner}
}

// Enabled reports whether inner handles records at level.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle appends the context's attributes to r and passes it to inner.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if attrs := Attrs(ctx); len(attrs) > 0 {
		r = r.Clone()
		r.AddAttrs(attrs...)
	}
	return h.inner.Handle(ctx, r)
}

// WithAttrs returns a Handler whose inner handler has attrs.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{inner: h.inner.WithAttrs(attrs)}
}

// WithGroup returns a Handler whose inner handler has the group name.
func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{inner: h.inner.WithGroup(name)}
}
