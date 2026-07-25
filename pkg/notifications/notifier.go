package notifications

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/sms"
	"github.com/Shamba-Records-Limited/microvault/pkg/phone"
)

// Notifier is a thin send-only interface that decouples notification logic from
// the underlying transport (SMS, push, email, etc.).
type Notifier interface {
	Send(ctx context.Context, to string, message string) error
}

// SMSNotifier implements Notifier by delegating to an sms.SMSProvider.
type SMSNotifier struct {
	provider sms.SMSProvider
	from     string
}

// Compile-time check.
var _ Notifier = (*SMSNotifier)(nil)

// NewSMSNotifier creates a new SMSNotifier.
func NewSMSNotifier(provider sms.SMSProvider, from string) *SMSNotifier {
	return &SMSNotifier{provider: provider, from: from}
}

// Send sends a single SMS message.
func (n *SMSNotifier) Send(ctx context.Context, to string, message string) error {
	slog.Info("sms: sending message",
		slog.String("to", phone.Redact(to)),
		slog.String("from", n.from),
		slog.Int("message_len", len(message)),
	)
	_, err := n.provider.SendSingleSMS(ctx, to, message, n.from)
	if err != nil {
		slog.Error("sms: send failed",
			slog.String("to", phone.Redact(to)),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("send SMS to %s: %w", phone.Redact(to), err)
	}
	slog.Info("sms: send succeeded",
		slog.String("to", phone.Redact(to)),
	)
	return nil
}

// NoOpNotifier silently discards all messages. Useful for testing and
// environments where notifications are not configured.
type NoOpNotifier struct{}

// Compile-time check.
var _ Notifier = (*NoOpNotifier)(nil)

// Send is a no-op.
func (*NoOpNotifier) Send(context.Context, string, string) error { return nil }
