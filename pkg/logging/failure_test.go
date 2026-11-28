package logging

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

func TestFailureMark(t *testing.T) {
	logger := New(io.Discard, slog.LevelDebug)
	cases := []struct {
		name string
		log  func(ctx context.Context)
		want bool
	}{
		{"nothing logged", func(context.Context) {}, false},
		{"a warning is not a failure", func(ctx context.Context) { logger.WarnContext(ctx, "slow") }, false},
		{"an error is", func(ctx context.Context) { logger.ErrorContext(ctx, "failed", "error", errors.New("x")) }, true},
		{"under a derived context too", func(ctx context.Context) {
			child, cancel := context.WithCancel(With(ctx, slog.String("k", "v")))
			defer cancel()
			logger.With("component", "x").ErrorContext(child, "failed")
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, failed := WithFailureMark(context.Background())
			tc.log(ctx)
			if got := failed(); got != tc.want {
				t.Fatalf("failed() = %v, want %v", got, tc.want)
			}
		})
	}
}

// An error logged without the mark's context leaves it untouched.
func TestFailureMark_OnlyItsOwnContext(t *testing.T) {
	logger := New(io.Discard, slog.LevelDebug)
	_, failed := WithFailureMark(context.Background())
	logger.ErrorContext(context.Background(), "elsewhere")
	if failed() {
		t.Fatal("an unrelated error set the mark")
	}
}
