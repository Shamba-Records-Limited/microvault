package database

import (
	"errors"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

const (
	tracerName  = "github.com/Shamba-Records-Limited/microvault/platform/database"
	spanInstKey = "otel:span"
)

// tracingPlugin opens a client span around each GORM operation that runs
// inside a traced context; untraced work gets no orphan root spans. The span
// records the SQL with its placeholders, never the bound values.
type tracingPlugin struct{}

func (tracingPlugin) Name() string { return "otel-tracing" }

func (tracingPlugin) Initialize(db *gorm.DB) error {
	cb := db.Callback()
	hooks := []struct {
		name     string
		register func(before, after func(*gorm.DB)) error
	}{
		{"create", func(b, a func(*gorm.DB)) error {
			return errors.Join(cb.Create().Before("gorm:create").Register("otel:before_create", b), cb.Create().After("gorm:create").Register("otel:after_create", a))
		}},
		{"query", func(b, a func(*gorm.DB)) error {
			return errors.Join(cb.Query().Before("gorm:query").Register("otel:before_query", b), cb.Query().After("gorm:query").Register("otel:after_query", a))
		}},
		{"update", func(b, a func(*gorm.DB)) error {
			return errors.Join(cb.Update().Before("gorm:update").Register("otel:before_update", b), cb.Update().After("gorm:update").Register("otel:after_update", a))
		}},
		{"delete", func(b, a func(*gorm.DB)) error {
			return errors.Join(cb.Delete().Before("gorm:delete").Register("otel:before_delete", b), cb.Delete().After("gorm:delete").Register("otel:after_delete", a))
		}},
		{"row", func(b, a func(*gorm.DB)) error {
			return errors.Join(cb.Row().Before("gorm:row").Register("otel:before_row", b), cb.Row().After("gorm:row").Register("otel:after_row", a))
		}},
		{"raw", func(b, a func(*gorm.DB)) error {
			return errors.Join(cb.Raw().Before("gorm:raw").Register("otel:before_raw", b), cb.Raw().After("gorm:raw").Register("otel:after_raw", a))
		}},
	}
	var errs []error
	for _, h := range hooks {
		errs = append(errs, h.register(startSpan(h.name), endSpan))
	}
	return errors.Join(errs...)
}

func startSpan(operation string) func(*gorm.DB) {
	return func(tx *gorm.DB) {
		ctx := tx.Statement.Context
		if ctx == nil || !trace.SpanFromContext(ctx).SpanContext().IsValid() {
			return
		}
		name := operation
		if table := tx.Statement.Table; table != "" {
			name = operation + " " + table
		}
		ctx, span := otel.Tracer(tracerName).Start(ctx, name,
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				attribute.String("db.system.name", "postgresql"),
				attribute.String("db.operation.name", strings.ToUpper(operation)),
			))
		if table := tx.Statement.Table; table != "" {
			span.SetAttributes(attribute.String("db.collection.name", table))
		}
		tx.Statement.Context = ctx
		tx.InstanceSet(spanInstKey, span)
	}
}

func endSpan(tx *gorm.DB) {
	v, ok := tx.InstanceGet(spanInstKey)
	if !ok {
		return
	}
	span, ok := v.(trace.Span)
	if !ok {
		return
	}
	defer span.End()
	span.SetAttributes(
		attribute.String("db.query.text", tx.Statement.SQL.String()),
		attribute.Int64("db.response.returned_rows", tx.Statement.RowsAffected),
	)
	if err := tx.Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}
