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
	notifier  Notifier
	templates *AccountTemplates
}

// Compile-time interface satisfaction check.
var _ contracts.AccountNotifier = (*SMSAccountNotifier)(nil)

// NewSMSAccountNotifier creates a new SMSAccountNotifier. If templates is nil,
// [DefaultAccountTemplates] is used.
func NewSMSAccountNotifier(notifier Notifier, templates *AccountTemplates) *SMSAccountNotifier {
	if templates == nil {
		templates = DefaultAccountTemplates()
	}
	return &SMSAccountNotifier{notifier: notifier, templates: templates}
}

// NotifyRegistrationSuccess sends a welcome message after successful registration.
func (s *SMSAccountNotifier) NotifyRegistrationSuccess(ctx context.Context, n contracts.AccountNotification) error {
	msg := fmt.Sprintf(s.templates.RegistrationSuccess, n.FullName)
	if err := s.notifier.Send(ctx, n.PhoneNumber, msg); err != nil {
		return fmt.Errorf("notify registration success for %s: %w", n.UserID, err)
	}
	return nil
}

// NotifyRegistrationFailed sends an alert when registration cannot be completed.
func (s *SMSAccountNotifier) NotifyRegistrationFailed(ctx context.Context, n contracts.AccountNotification) error {
	msg := fmt.Sprintf(s.templates.RegistrationFailed, n.Reason)
	if err := s.notifier.Send(ctx, n.PhoneNumber, msg); err != nil {
		return fmt.Errorf("notify registration failed for %s: %w", n.UserID, err)
	}
	return nil
}

// NotifyPINWrongAttempt sends a warning after an incorrect PIN entry.
func (s *SMSAccountNotifier) NotifyPINWrongAttempt(ctx context.Context, n contracts.AccountNotification) error {
	msg := fmt.Sprintf(s.templates.WrongAttempt, n.RemainingAttempts)
	if err := s.notifier.Send(ctx, n.PhoneNumber, msg); err != nil {
		return fmt.Errorf("notify PIN wrong attempt for %s: %w", n.UserID, err)
	}
	return nil
}

// NotifyAccountLocked sends a security alert when the account is locked.
func (s *SMSAccountNotifier) NotifyAccountLocked(ctx context.Context, n contracts.AccountNotification) error {
	msg := fmt.Sprintf(s.templates.AccountLocked, n.LockedUntil)
	if err := s.notifier.Send(ctx, n.PhoneNumber, msg); err != nil {
		return fmt.Errorf("notify account locked for %s: %w", n.UserID, err)
	}
	return nil
}

// NotifyPINChanged sends a confirmation after a successful PIN change.
func (s *SMSAccountNotifier) NotifyPINChanged(ctx context.Context, n contracts.AccountNotification) error {
	if err := s.notifier.Send(ctx, n.PhoneNumber, s.templates.PINChanged); err != nil {
		return fmt.Errorf("notify PIN changed for %s: %w", n.UserID, err)
	}
	return nil
}

// NotifyPINChangeFailed sends an alert when a PIN change attempt is unsuccessful.
func (s *SMSAccountNotifier) NotifyPINChangeFailed(ctx context.Context, n contracts.AccountNotification) error {
	msg := fmt.Sprintf(s.templates.PINChangeFailed, n.Reason)
	if err := s.notifier.Send(ctx, n.PhoneNumber, msg); err != nil {
		return fmt.Errorf("notify PIN change failed for %s: %w", n.UserID, err)
	}
	return nil
}

// NotifyPINReset sends a confirmation after a successful PIN reset.
func (s *SMSAccountNotifier) NotifyPINReset(ctx context.Context, n contracts.AccountNotification) error {
	if err := s.notifier.Send(ctx, n.PhoneNumber, s.templates.PINReset); err != nil {
		return fmt.Errorf("notify PIN reset for %s: %w", n.UserID, err)
	}
	return nil
}

// NotifyPINResetFailed sends an alert when a PIN reset attempt fails.
func (s *SMSAccountNotifier) NotifyPINResetFailed(ctx context.Context, n contracts.AccountNotification) error {
	msg := fmt.Sprintf(s.templates.PINResetFailed, n.Reason)
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
