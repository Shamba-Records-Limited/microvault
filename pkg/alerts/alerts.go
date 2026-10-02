// Package alerts raises ops alerts: things a human must act on. Every alert
// is emitted as one structured log line, which OpenObserve alert rules match:
//
//	level=ERROR msg="ops alert" alert_subject=… alert_message=…
//
// plus whatever the context carries through logging.With (loan_id,
// loan_reference, trace_id). Delivery to Slack, Telegram and PagerDuty is
// OpenObserve's job, not the code's.
package alerts

import (
	"context"
	"log/slog"
)

// Message and attribute keys OpenObserve alert rules match on. Changing any
// of them silently breaks alerting.
const (
	Message     = "ops alert"
	AttrSubject = "alert_subject"
	AttrMessage = "alert_message"
)

// Service raises an ops alert. subject is a stable, low-cardinality title
// that alert rules route on; message carries the specifics.
type Service interface {
	AlertOps(ctx context.Context, subject, message string) error
}

// LogAlerter raises an alert as the canonical log line.
type LogAlerter struct {
	Logger *slog.Logger
}

// AlertOps implements Service. It never fails.
func (l LogAlerter) AlertOps(ctx context.Context, subject, message string) error {
	logger := l.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.ErrorContext(ctx, Message, AttrSubject, subject, AttrMessage, message)
	return nil
}

// Raise sends an alert through svc. A nil svc, or one that fails, falls back
// to the log line, so an alert is never lost to a delivery problem.
func Raise(ctx context.Context, svc Service, logger *slog.Logger, subject, message string) {
	fallback := LogAlerter{Logger: logger}
	if svc == nil {
		_ = fallback.AlertOps(ctx, subject, message)
		return
	}
	if err := svc.AlertOps(ctx, subject, message); err != nil {
		if logger == nil {
			logger = slog.Default()
		}
		logger.WarnContext(ctx, "ops alert delivery failed, logging it instead", AttrSubject, subject, "error", err)
		_ = fallback.AlertOps(ctx, subject, message)
	}
}
