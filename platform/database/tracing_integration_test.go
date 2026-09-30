//go:build integration

package database

import (
	"context"
	"os"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/Shamba-Records-Limited/microvault/pkg/config"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func TestTracingPluginRecordsStatementWithoutValues(t *testing.T) {
	cfg := &config.PostgresConfig{
		Host:     env("DB_HOST", "localhost"),
		Port:     env("DB_PORT", "5435"),
		User:     env("DB_USER", "microvault_test"),
		Password: env("DB_PASSWORD", "microvault_test"),
		DBName:   env("DB_NAME", "microvault_test"),
		SSLMode:  "disable",
		TimeZone: "UTC",
	}
	db, err := GetConnection("tracing-test", cfg)
	if err != nil {
		t.Skipf("no database: %v", err)
	}

	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev); _ = tp.Shutdown(context.Background()) })

	var n int
	if err := db.WithContext(context.Background()).Raw("SELECT ?::int", 41).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	if got := len(rec.Ended()); got != 0 {
		t.Fatalf("untraced query produced %d spans", got)
	}

	ctx, parent := tp.Tracer("test").Start(context.Background(), "parent")
	if err := db.WithContext(ctx).Raw("SELECT ?::int", 42).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	parent.End()

	var dbSpan sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.Name() != "parent" {
			dbSpan = s
		}
	}
	if dbSpan == nil {
		t.Fatal("no database span recorded")
	}
	if dbSpan.Parent().SpanID() != parent.SpanContext().SpanID() {
		t.Error("database span is not a child of the caller's span")
	}
	for _, kv := range dbSpan.Attributes() {
		if kv.Key == "db.query.text" {
			if q := kv.Value.AsString(); strings.Contains(q, "42") || !strings.Contains(q, "$1") {
				t.Errorf("db.query.text = %q, want placeholder and no value", q)
			}
			return
		}
	}
	t.Error("db.query.text not recorded")
}
