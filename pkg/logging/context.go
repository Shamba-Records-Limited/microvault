package logging

import (
	"context"
	"log/slog"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
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

// WithLoan attaches a loan's identity. Empty values are skipped, so callers
// that only know the id need not special-case the reference.
func WithLoan(ctx context.Context, loanID, reference string) context.Context {
	var attrs []slog.Attr
	if loanID != "" {
		attrs = append(attrs, slog.String(pkgErrors.AttrLoanID, loanID))
	}
	if reference != "" {
		attrs = append(attrs, slog.String(pkgErrors.AttrLoanReference, reference))
	}
	return With(ctx, attrs...)
}

// Attrs returns the attributes attached to ctx by With.
func Attrs(ctx context.Context) []slog.Attr {
	if ctx == nil {
		return nil
	}
	attrs, _ := ctx.Value(attrsKey{}).([]slog.Attr)
	return attrs
}
