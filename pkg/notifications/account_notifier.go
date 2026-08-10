package notifications

import (
	"context"
	"fmt"

	"github.com/Shamba-Records-Limited/microvault/pkg/contracts"
)

// SMSAccountNotifier implements [contracts.AccountNotifier] by formatting
// notification messages from [AccountTemplates] and delivering them through
// a [Notifier] transport (typically SMS).
type SMSAccountNotifier struct {
	notifier    Notifier
	templates   map[string]*AccountTemplates
	resolveLang LanguageResolver
}

// Compile-time interface satisfaction check.
var _ contracts.AccountNotifier = (*SMSAccountNotifier)(nil)

// NewSMSAccountNotifier creates a new SMSAccountNotifier. If templates is nil,
// the built-in localized templates (en/sw/fr) are used. A non-nil templates
// value is used for every language (single-language override).
func NewSMSAccountNotifier(notifier Notifier, templates *AccountTemplates) *SMSAccountNotifier {
	set := localizedAccountTemplates()
	if templates != nil {
		set = map[string]*AccountTemplates{"en": templates, "sw": templates, "fr": templates}
	}
	return &SMSAccountNotifier{notifier: notifier, templates: set}
}

// SetLanguageResolver injects a resolver consulted when a notification leaves
// Language empty. Pass nil to fall back to English.
func (s *SMSAccountNotifier) SetLanguageResolver(r LanguageResolver) {
	s.resolveLang = r
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

// NotifyRegistrationSuccess sends a welcome message after successful registration.
func (s *SMSAccountNotifier) NotifyRegistrationSuccess(ctx context.Context, n contracts.AccountNotification) error {
	msg := fmt.Sprintf(s.tmpl(ctx, n).RegistrationSuccess, n.FullName)
	if err := s.notifier.Send(ctx, n.PhoneNumber, msg); err != nil {
		return fmt.Errorf("notify registration success for %s: %w", n.UserID, err)
	}
	return nil
}

// NotifyRegistrationFailed sends an alert when registration cannot be completed.
func (s *SMSAccountNotifier) NotifyRegistrationFailed(ctx context.Context, n contracts.AccountNotification) error {
	msg := fmt.Sprintf(s.tmpl(ctx, n).RegistrationFailed, n.Reason)
	if err := s.notifier.Send(ctx, n.PhoneNumber, msg); err != nil {
		return fmt.Errorf("notify registration failed for %s: %w", n.UserID, err)
	}
	return nil
}

// NotifyPINWrongAttempt sends a warning after an incorrect PIN entry.
func (s *SMSAccountNotifier) NotifyPINWrongAttempt(ctx context.Context, n contracts.AccountNotification) error {
	msg := fmt.Sprintf(s.tmpl(ctx, n).WrongAttempt, n.RemainingAttempts)
	if err := s.notifier.Send(ctx, n.PhoneNumber, msg); err != nil {
		return fmt.Errorf("notify PIN wrong attempt for %s: %w", n.UserID, err)
	}
	return nil
}

// NotifyAccountLocked sends a security alert when the account is locked.
func (s *SMSAccountNotifier) NotifyAccountLocked(ctx context.Context, n contracts.AccountNotification) error {
	msg := fmt.Sprintf(s.tmpl(ctx, n).AccountLocked, n.LockedUntil)
	if err := s.notifier.Send(ctx, n.PhoneNumber, msg); err != nil {
		return fmt.Errorf("notify account locked for %s: %w", n.UserID, err)
	}
	return nil
}

// NotifyPINChanged sends a confirmation after a successful PIN change.
func (s *SMSAccountNotifier) NotifyPINChanged(ctx context.Context, n contracts.AccountNotification) error {
	if err := s.notifier.Send(ctx, n.PhoneNumber, s.tmpl(ctx, n).PINChanged); err != nil {
		return fmt.Errorf("notify PIN changed for %s: %w", n.UserID, err)
	}
	return nil
}

// NotifyPINChangeFailed sends an alert when a PIN change attempt is unsuccessful.
func (s *SMSAccountNotifier) NotifyPINChangeFailed(ctx context.Context, n contracts.AccountNotification) error {
	msg := fmt.Sprintf(s.tmpl(ctx, n).PINChangeFailed, n.Reason)
	if err := s.notifier.Send(ctx, n.PhoneNumber, msg); err != nil {
		return fmt.Errorf("notify PIN change failed for %s: %w", n.UserID, err)
	}
	return nil
}

// NotifyPINReset sends a confirmation after a successful PIN reset.
func (s *SMSAccountNotifier) NotifyPINReset(ctx context.Context, n contracts.AccountNotification) error {
	if err := s.notifier.Send(ctx, n.PhoneNumber, s.tmpl(ctx, n).PINReset); err != nil {
		return fmt.Errorf("notify PIN reset for %s: %w", n.UserID, err)
	}
	return nil
}

// NotifyPINResetFailed sends an alert when a PIN reset attempt fails.
func (s *SMSAccountNotifier) NotifyPINResetFailed(ctx context.Context, n contracts.AccountNotification) error {
	msg := fmt.Sprintf(s.tmpl(ctx, n).PINResetFailed, n.Reason)
	if err := s.notifier.Send(ctx, n.PhoneNumber, msg); err != nil {
		return fmt.Errorf("notify PIN reset failed for %s: %w", n.UserID, err)
	}
	return nil
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
