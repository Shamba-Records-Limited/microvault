package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

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
