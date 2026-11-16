package sms

import (
	"context"
	"log/slog"

	"github.com/Shamba-Records-Limited/microvault/pkg/phone"
)

// DeliveryReportHandler processes SMS delivery report callbacks.
type DeliveryReportHandler struct {
	onReport func(ctx context.Context, report DeliveryReport)
}

// DeliveryReportOption configures a DeliveryReportHandler.
type DeliveryReportOption func(*DeliveryReportHandler)

// WithReportHandler sets a custom handler function for delivery reports.
func WithReportHandler(fn func(ctx context.Context, report DeliveryReport)) DeliveryReportOption {
	return func(h *DeliveryReportHandler) {
		h.onReport = fn
	}
}

// NewDeliveryReportHandler creates a new DeliveryReportHandler.
// By default it logs reports using structured logging.
func NewDeliveryReportHandler(opts ...DeliveryReportOption) *DeliveryReportHandler {
	h := &DeliveryReportHandler{
		onReport: defaultReportHandler,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// HandleReport processes a single delivery report.
func (h *DeliveryReportHandler) HandleReport(ctx context.Context, report DeliveryReport) {
	h.onReport(ctx, report)
}

func defaultReportHandler(ctx context.Context, report DeliveryReport) {
	attrs := []any{
		slog.String("message_id", report.ID),
		slog.String("status", report.Status),
		slog.String("phone_number", phone.Redact(report.PhoneNumber)),
		slog.String("network_code", report.NetworkCode),
		slog.Bool("final", report.IsFinalStatus()),
	}
	if report.FailureReason != "" {
		attrs = append(attrs, slog.String("failure_reason", report.FailureReason))
	}
	slog.InfoContext(ctx, "sms delivery report received", attrs...)
}

// WithLog applies each report to l as well as logging it.
func WithLog(l *Log) DeliveryReportOption {
	return WithReportHandler(func(ctx context.Context, report DeliveryReport) {
		defaultReportHandler(ctx, report)
		changed, err := l.ApplyReport(ctx, report)
		switch {
		case err != nil:
			slog.WarnContext(ctx, "sms delivery report not recorded",
				slog.String("message_id", report.ID), slog.String("error", err.Error()))
		case !changed:
			slog.InfoContext(ctx, "sms delivery report matched no open message",
				slog.String("message_id", report.ID), slog.String("status", report.Status))
		}
	})
}
