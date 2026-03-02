// Package notifications provides base notification primitives and loan lifecycle
// notifiers. Consumers depend on the Notifier interface
// for transport and on contracts.LoanNotifier for loan-specific messaging.
package notifications

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/sms"
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
		slog.String("to", redactPhone(to)),
		slog.String("from", n.from),
		slog.Int("message_len", len(message)),
	)
	_, err := n.provider.SendSingleSMS(ctx, to, message, n.from)
	if err != nil {
		slog.Error("sms: send failed",
			slog.String("to", redactPhone(to)),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("send SMS to %s: %w", redactPhone(to), err)
	}
	slog.Info("sms: send succeeded",
		slog.String("to", redactPhone(to)),
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

// redactPhone masks the middle digits of a phone number for safe logging.
// e.g. "254799334972" → "2547XXXX972", "+254799334972" → "+2547XXXX972"
func redactPhone(phone string) string {
	digits := phone
	prefix := ""
	if len(phone) > 0 && phone[0] == '+' {
		prefix = "+"
		digits = phone[1:]
	}
	if len(digits) <= 6 {
		return phone
	}
	return prefix + digits[:4] + strings.Repeat("X", len(digits)-7) + digits[len(digits)-3:]
}

// FormatPhoneNumber cleans a phone number string and prepends a country code
// when one is not already present.
func FormatPhoneNumber(phoneNumber, countryCode string) string {
	cleaned := ""
	for _, r := range phoneNumber {
		if r >= '0' && r <= '9' {
			cleaned += string(r)
		}
	}

	if len(cleaned) > 0 && cleaned[0] != '+' && !(len(cleaned) >= 2 && cleaned[:2] == "00") {
		if countryCode == "" {
			countryCode = "+254" // Default to Kenya
		}
		cleaned = countryCode + cleaned
	}

	return cleaned
}
