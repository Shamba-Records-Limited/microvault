package logging

import (
	"context"
	"sync/atomic"
)

type failureKey struct{}

// WithFailureMark returns a context carrying a fresh mark, and a function
// reporting whether an error-level record was logged under that context, or
// any context derived from it, since. Background work uses it to tell failed
// units from successful ones, since most report failure by logging.
func WithFailureMark(ctx context.Context) (marked context.Context, failed func() bool) {
	mark := new(atomic.Bool)
	return context.WithValue(ctx, failureKey{}, mark), mark.Load
}

func markFailure(ctx context.Context) {
	if mark, ok := ctx.Value(failureKey{}).(*atomic.Bool); ok {
		mark.Store(true)
	}
}
