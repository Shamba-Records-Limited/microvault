package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/Shamba-Records-Limited/microvault/pkg/logging"
)

func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", buf.String(), err)
	}
	return out
}

func TestContextAttrsAppearOnRecord(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(&buf, slog.LevelInfo)

	ctx := logging.With(context.Background(), slog.String("request_id", "r1"))
	ctx = logging.With(ctx, slog.String("loan_id", "l1"))
	logger.InfoContext(ctx, "hello", slog.Int("n", 1))

	got := decode(t, &buf)
	for k, want := range map[string]any{"msg": "hello", "request_id": "r1", "loan_id": "l1", "n": float64(1)} {
		if got[k] != want {
			t.Errorf("%s = %v, want %v", k, got[k], want)
		}
	}
}

func TestWithDoesNotLeakIntoParent(t *testing.T) {
	parent := logging.With(context.Background(), slog.String("a", "1"))
	_ = logging.With(parent, slog.String("b", "2"))

	if n := len(logging.Attrs(parent)); n != 1 {
		t.Fatalf("parent has %d attrs, want 1", n)
	}
}

func TestSiblingContextsDoNotShareBackingArray(t *testing.T) {
	parent := logging.With(context.Background(), slog.String("a", "1"))
	left := logging.With(parent, slog.String("side", "left"))
	right := logging.With(parent, slog.String("side", "right"))

	if v := logging.Attrs(left)[1].Value.String(); v != "left" {
		t.Fatalf("left side = %q", v)
	}
	if v := logging.Attrs(right)[1].Value.String(); v != "right" {
		t.Fatalf("right side = %q", v)
	}
}

func TestRecordWithoutContextAttrsIsUnchanged(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(&buf, slog.LevelInfo)
	logger.InfoContext(context.Background(), "plain")

	got := decode(t, &buf)
	if len(got) != 3 {
		t.Fatalf("got %v, want only time, level, msg", got)
	}
}

func TestLoggerWithKeepsContextAttrs(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(&buf, slog.LevelInfo).With(slog.String("component", "poller"))

	ctx := logging.With(context.Background(), slog.String("request_id", "r1"))
	logger.InfoContext(ctx, "tick")

	got := decode(t, &buf)
	if got["component"] != "poller" || got["request_id"] != "r1" {
		t.Fatalf("got %v", got)
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(&buf, slog.LevelWarn)
	logger.InfoContext(context.Background(), "dropped")
	if buf.Len() != 0 {
		t.Fatalf("info logged at warn level: %s", buf.String())
	}
}

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in   string
		want slog.Level
	}{
		{"", slog.LevelInfo},
		{"debug", slog.LevelDebug},
		{"WARN", slog.LevelWarn},
		{"\terror\n", slog.LevelError},
	}
	for _, c := range cases {
		got, err := logging.ParseLevel(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseLevel(%q) = %v, %v; want %v", c.in, got, err, c.want)
		}
	}
	if _, err := logging.ParseLevel("loud"); err == nil {
		t.Error("ParseLevel(loud) succeeded")
	}
}

func TestTraceIDsAppearOnlyWithAValidSpan(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(&buf, slog.LevelInfo)

	logger.InfoContext(context.Background(), "untraced")
	if got := decode(t, &buf); got["trace_id"] != nil {
		t.Fatalf("trace_id without a span: %v", got)
	}

	buf.Reset()
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x0a, 0x0b},
		SpanID:     trace.SpanID{0x01},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)
	logger.InfoContext(ctx, "traced")
	got := decode(t, &buf)
	if got["trace_id"] != sc.TraceID().String() || got["span_id"] != sc.SpanID().String() {
		t.Fatalf("got %v", got)
	}
}

func TestWithLoan_SkipsEmptyValues(t *testing.T) {
	tests := []struct {
		name      string
		id, ref   string
		wantAttrs []slog.Attr
	}{
		{name: "both", id: "l1", ref: "R1", wantAttrs: []slog.Attr{slog.String("loan_id", "l1"), slog.String("loan_reference", "R1")}},
		{name: "id only", id: "l1", wantAttrs: []slog.Attr{slog.String("loan_id", "l1")}},
		{name: "neither", wantAttrs: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := logging.WithLoan(context.Background(), tt.id, tt.ref)
			if diff := len(logging.Attrs(ctx)) - len(tt.wantAttrs); diff != 0 {
				t.Fatalf("got %v, want %v", logging.Attrs(ctx), tt.wantAttrs)
			}
			for i, a := range tt.wantAttrs {
				if !logging.Attrs(ctx)[i].Equal(a) {
					t.Fatalf("attr %d: got %v, want %v", i, logging.Attrs(ctx)[i], a)
				}
			}
		})
	}
}
