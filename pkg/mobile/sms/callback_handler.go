package sms

import (
	"context"
	"log/slog"
	"strings"
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

func defaultReportHandler(_ context.Context, report DeliveryReport) {
	attrs := []any{
		slog.String("message_id", report.ID),
		slog.String("status", report.Status),
		slog.String("phone_number", redactPhone(report.PhoneNumber)),
		slog.String("network_code", report.NetworkCode),
		slog.Bool("final", report.IsFinalStatus()),
	}
	if report.FailureReason != "" {
		attrs = append(attrs, slog.String("failure_reason", report.FailureReason))
	}
	slog.Info("sms delivery report received", attrs...)
}

// redactPhone masks the middle digits of a phone number for safe logging.
func redactPhone(phone string) string {
	digits := phone
	prefix := ""
	if strings.HasPrefix(phone, "+") {
		prefix = "+"
		digits = phone[1:]
	}
	if len(digits) <= 6 {
		return phone
	}
	return prefix + digits[:4] + strings.Repeat("X", len(digits)-7) + digits[len(digits)-3:]
}
