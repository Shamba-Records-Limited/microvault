package notifications

import (
	"context"
	"fmt"

	"github.com/Shamba-Records-Limited/microvault/pkg/contracts"
)

// SMSAccountNotifier implements [contracts.AccountNotifier] by rendering
// messages from [AccountTemplates] and delivering them through a [Notifier]
// transport (typically SMS).
type SMSAccountNotifier struct {
	notifier    Notifier
	templates   map[string]*AccountTemplates
	resolveLang LanguageResolver
}

// Compile-time interface satisfaction check.
var _ contracts.AccountNotifier = (*SMSAccountNotifier)(nil)

// NewSMSAccountNotifier creates a new SMSAccountNotifier over the built-in
// localized templates (en/sw/fr), adjusted by any options. Templates are
// validated against [SentinelAccountNotification] before it returns.
func NewSMSAccountNotifier(notifier Notifier, opts ...AccountOption) (*SMSAccountNotifier, error) {
	var cfg accountConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	set := localizedAccountTemplates()
	for lang, override := range cfg.overrides {
		base, ok := set[lang]
		if !ok {
			return nil, fmt.Errorf("account templates: unsupported language %q", lang)
		}
		set[lang] = mergeInto(base, override)
	}
	for lang, merged := range set {
		if err := validateAccountTemplates(lang, merged); err != nil {
			return nil, err
		}
	}

	return &SMSAccountNotifier{notifier: notifier, templates: set, resolveLang: cfg.resolve}, nil
}

// tmpl selects the template set for a notification: its pinned Language, else
// the resolved recipient preference, else English.
func (s *SMSAccountNotifier) tmpl(ctx context.Context, n contracts.AccountNotification) *AccountTemplates {
	lang := n.Language
	if lang == "" && s.resolveLang != nil {
		lang = s.resolveLang(ctx, n.PhoneNumber)
	}
	if t, ok := s.templates[lang]; ok {
		return t
	}
	return s.templates["en"]
}

// send renders one message and wraps any transport failure with the event name.
func (s *SMSAccountNotifier) send(ctx context.Context, n contracts.AccountNotification, event string, msg AccountMessage) error {
	if err := s.notifier.Send(ctx, n.PhoneNumber, msg(n)); err != nil {
		return fmt.Errorf("notify %s for %s: %w", event, n.UserID, err)
	}
	return nil
}

// NotifyRegistrationSuccess sends a welcome message after successful registration.
func (s *SMSAccountNotifier) NotifyRegistrationSuccess(ctx context.Context, n contracts.AccountNotification) error {
	return s.send(ctx, n, "registration success", s.tmpl(ctx, n).RegistrationSuccess)
}

// NotifyRegistrationFailed sends an alert when registration cannot be completed.
func (s *SMSAccountNotifier) NotifyRegistrationFailed(ctx context.Context, n contracts.AccountNotification) error {
	return s.send(ctx, n, "registration failed", s.tmpl(ctx, n).RegistrationFailed)
}

// NotifyPINWrongAttempt sends a warning after an incorrect PIN entry.
func (s *SMSAccountNotifier) NotifyPINWrongAttempt(ctx context.Context, n contracts.AccountNotification) error {
	return s.send(ctx, n, "PIN wrong attempt", s.tmpl(ctx, n).WrongAttempt)
}

// NotifyAccountLocked sends a security alert when the account is locked.
func (s *SMSAccountNotifier) NotifyAccountLocked(ctx context.Context, n contracts.AccountNotification) error {
	return s.send(ctx, n, "account locked", s.tmpl(ctx, n).AccountLocked)
}

// NotifyPINChanged sends a confirmation after a successful PIN change.
func (s *SMSAccountNotifier) NotifyPINChanged(ctx context.Context, n contracts.AccountNotification) error {
	return s.send(ctx, n, "PIN changed", s.tmpl(ctx, n).PINChanged)
}

// NotifyPINChangeFailed sends an alert when a PIN change attempt is unsuccessful.
func (s *SMSAccountNotifier) NotifyPINChangeFailed(ctx context.Context, n contracts.AccountNotification) error {
	return s.send(ctx, n, "PIN change failed", s.tmpl(ctx, n).PINChangeFailed)
}

// NotifyPINReset sends a confirmation after a successful PIN reset.
func (s *SMSAccountNotifier) NotifyPINReset(ctx context.Context, n contracts.AccountNotification) error {
	return s.send(ctx, n, "PIN reset", s.tmpl(ctx, n).PINReset)
}

// NotifyPINResetFailed sends an alert when a PIN reset attempt fails.
func (s *SMSAccountNotifier) NotifyPINResetFailed(ctx context.Context, n contracts.AccountNotification) error {
	return s.send(ctx, n, "PIN reset failed", s.tmpl(ctx, n).PINResetFailed)
}

// NoOpAccountNotifier silently discards all account notifications. It is useful
// for testing and environments where SMS delivery is not configured.
type NoOpAccountNotifier struct{}

// Compile-time interface satisfaction check.
var _ contracts.AccountNotifier = (*NoOpAccountNotifier)(nil)

func (*NoOpAccountNotifier) NotifyRegistrationSuccess(context.Context, contracts.AccountNotification) error {
	return nil
}
func (*NoOpAccountNotifier) NotifyRegistrationFailed(context.Context, contracts.AccountNotification) error {
	return nil
}
func (*NoOpAccountNotifier) NotifyPINWrongAttempt(context.Context, contracts.AccountNotification) error {
	return nil
}
func (*NoOpAccountNotifier) NotifyAccountLocked(context.Context, contracts.AccountNotification) error {
	return nil
}
func (*NoOpAccountNotifier) NotifyPINChanged(context.Context, contracts.AccountNotification) error {
	return nil
}
func (*NoOpAccountNotifier) NotifyPINChangeFailed(context.Context, contracts.AccountNotification) error {
	return nil
}
func (*NoOpAccountNotifier) NotifyPINReset(context.Context, contracts.AccountNotification) error {
	return nil
}
func (*NoOpAccountNotifier) NotifyPINResetFailed(context.Context, contracts.AccountNotification) error {
	return nil
}
