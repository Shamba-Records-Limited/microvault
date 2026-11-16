package notifications

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/sms"
	"github.com/Shamba-Records-Limited/microvault/pkg/phone"
)

// Message is one outbound text. Kind names what it is ("loan.disbursed") and
// Ref what it is about (a loan ID); both are recorded with the send so its
// delivery can be traced. The body is never recorded.
type Message struct {
	To   string
	Body string
	Kind string
	Ref  string
}

// Notifier is a thin send-only interface that decouples notification logic from
// the underlying transport (SMS, push, email, etc.).
type Notifier interface {
	Send(ctx context.Context, m Message) error
}

// ErrRejected is a send the gateway refused for its recipient, such as an
// invalid or blacklisted number.
var ErrRejected = errors.New("sms rejected by the gateway")

// SendRecorder keeps a record of each send.
type SendRecorder interface {
	Record(ctx context.Context, m *sms.Message) error
}

// SMSNotifier implements Notifier by delegating to an sms.SMSProvider.
type SMSNotifier struct {
	provider sms.SMSProvider
	from     string
	recorder SendRecorder
	name     string
}

// Compile-time check.
var _ Notifier = (*SMSNotifier)(nil)

// NotifierOption configures an SMSNotifier.
type NotifierOption func(*SMSNotifier)

// WithSendRecorder records every send, accepted or not, under the provider
// name given.
func WithSendRecorder(r SendRecorder, provider string) NotifierOption {
	return func(n *SMSNotifier) {
		n.recorder = r
		n.name = provider
	}
}

// NewSMSNotifier creates a new SMSNotifier.
func NewSMSNotifier(provider sms.SMSProvider, from string, opts ...NotifierOption) *SMSNotifier {
	n := &SMSNotifier{provider: provider, from: from}
	for _, opt := range opts {
		opt(n)
	}
	return n
}

// Send sends a single SMS message.
func (n *SMSNotifier) Send(ctx context.Context, m Message) error {
	slog.InfoContext(ctx, "sms: sending message",
		slog.String("to", phone.Redact(m.To)),
		slog.String("kind", m.Kind),
		slog.String("from", n.from),
		slog.Int("message_len", len(m.Body)),
	)
	resp, err := n.provider.SendSingleSMS(ctx, m.To, m.Body, n.from)
	if err != nil {
		n.record(ctx, m, sms.Recipient{}, sms.StatusSendFailed, err.Error())
		return fmt.Errorf("send SMS to %s: %w", phone.Redact(m.To), err)
	}
	if len(resp.Recipients) == 0 {
		n.record(ctx, m, sms.Recipient{}, sms.StatusSubmitted, "")
	}
	var rejected *sms.Recipient
	for i, r := range resp.Recipients {
		if r.Accepted() {
			n.record(ctx, m, r, sms.StatusSubmitted, "")
			continue
		}
		n.record(ctx, m, r, sms.StatusRejected, r.Status)
		rejected = &resp.Recipients[i]
	}
	if rejected != nil {
		return fmt.Errorf("send SMS to %s: %w: %s", phone.Redact(m.To), ErrRejected, rejected.Status)
	}
	slog.InfoContext(ctx, "sms: send succeeded",
		slog.String("to", phone.Redact(m.To)),
		slog.String("kind", m.Kind),
	)
	return nil
}

func (n *SMSNotifier) record(ctx context.Context, m Message, r sms.Recipient, status, reason string) {
	if n.recorder == nil {
		return
	}
	row := &sms.Message{
		Provider: n.name, PhoneNumber: m.To, Kind: m.Kind, Ref: m.Ref,
		Status: status, FailureReason: reason, Cost: r.Cost,
	}
	if r.MessageID != "" && r.MessageID != "None" {
		id := r.MessageID
		row.ProviderMessageID = &id
	}
	if err := n.recorder.Record(context.WithoutCancel(ctx), row); err != nil {
		slog.WarnContext(ctx, "sms: could not record the send",
			slog.String("kind", m.Kind), slog.String("error", err.Error()))
	}
}

// NoOpNotifier silently discards all messages. Useful for testing and
// environments where notifications are not configured.
type NoOpNotifier struct{}

// Compile-time check.
var _ Notifier = (*NoOpNotifier)(nil)

// Send is a no-op.
func (*NoOpNotifier) Send(context.Context, Message) error { return nil }
