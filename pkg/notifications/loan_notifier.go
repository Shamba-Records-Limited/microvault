package notifications

import (
	"context"
	"fmt"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/contracts"
)

// LanguageResolver returns the preferred SMS language (ISO code en/sw/fr) for a
// recipient phone number. Used by notifiers when a notification doesn't pin its
// own Language.
type LanguageResolver func(ctx context.Context, phoneNumber string) string

// SMSLoanNotifier implements contracts.LoanNotifier using a Notifier transport
// and per-language LoanTemplates.
type SMSLoanNotifier struct {
	notifier    Notifier
	templates   map[string]*LoanTemplates
	resolveLang LanguageResolver
}

// Compile-time check.
var _ contracts.LoanNotifier = (*SMSLoanNotifier)(nil)

// NewSMSLoanNotifier creates a new SMSLoanNotifier. Pass nil for templates to
// use the built-in localized templates (en/sw/fr). A non-nil templates value
// is used for every language (single-language override).
func NewSMSLoanNotifier(notifier Notifier, templates *LoanTemplates) *SMSLoanNotifier {
	set := localizedLoanTemplates()
	if templates != nil {
		set = map[string]*LoanTemplates{"en": templates, "sw": templates, "fr": templates}
	}
	return &SMSLoanNotifier{notifier: notifier, templates: set}
}

// SetLanguageResolver injects a resolver consulted when a notification leaves
// Language empty. Pass nil to fall back to English.
func (s *SMSLoanNotifier) SetLanguageResolver(r LanguageResolver) {
	s.resolveLang = r
}

// tmpl selects the template set for a notification: its pinned Language, else
// the resolved recipient preference, else English.
func (s *SMSLoanNotifier) tmpl(ctx context.Context, n contracts.LoanNotification) *LoanTemplates {
	lang := n.Language
	if lang == "" && s.resolveLang != nil {
		lang = s.resolveLang(ctx, n.PhoneNumber)
	}
	if t, ok := s.templates[lang]; ok {
		return t
	}
	return s.templates["en"]
}

func (s *SMSLoanNotifier) NotifyLoanApproved(ctx context.Context, n contracts.LoanNotification) error {
	msg := fmt.Sprintf(s.tmpl(ctx, n).Approved, n.DisplayCurrency, n.DisplayAmount, n.LoanReference)
	return s.notifier.Send(ctx, n.PhoneNumber, msg)
}

func (s *SMSLoanNotifier) NotifyLoanRejected(ctx context.Context, n contracts.LoanNotification) error {
	msg := fmt.Sprintf(s.tmpl(ctx, n).Rejected, n.DisplayCurrency, n.DisplayAmount, n.Reason)
	return s.notifier.Send(ctx, n.PhoneNumber, msg)
}

func (s *SMSLoanNotifier) NotifyLoanDisbursed(ctx context.Context, n contracts.LoanNotification) error {
	msg := fmt.Sprintf(s.tmpl(ctx, n).Disbursed, n.DisplayCurrency, n.DisplayAmount, n.LoanReference)
	return s.notifier.Send(ctx, n.PhoneNumber, msg)
}

func (s *SMSLoanNotifier) NotifyLoanFailed(ctx context.Context, n contracts.LoanNotification) error {
	msg := fmt.Sprintf(s.tmpl(ctx, n).Failed, n.LoanReference)
	return s.notifier.Send(ctx, n.PhoneNumber, msg)
}

func (s *SMSLoanNotifier) NotifyLoanOffRampFailed(ctx context.Context, n contracts.LoanNotification) error {
	msg := fmt.Sprintf(s.tmpl(ctx, n).OffRampFailed, n.DisplayCurrency, n.DisplayAmount, n.LoanReference)
	return s.notifier.Send(ctx, n.PhoneNumber, msg)
}

func (s *SMSLoanNotifier) NotifyLoanCashPickupApproved(ctx context.Context, n contracts.LoanNotification) error {
	msg := fmt.Sprintf(s.tmpl(ctx, n).CashPickupApproved, n.DisplayCurrency, n.DisplayAmount, n.LoanReference)
	return s.notifier.Send(ctx, n.PhoneNumber, msg)
}

func (s *SMSLoanNotifier) NotifyRepaymentReceived(ctx context.Context, n contracts.LoanNotification) error {
	msg := fmt.Sprintf(s.tmpl(ctx, n).RepaymentReceived,
		n.DisplayCurrency, n.DisplayAmount, n.LoanReference,
		n.DisplayCurrency, n.RemainingBalance)
	return s.notifier.Send(ctx, n.PhoneNumber, msg)
}

func (s *SMSLoanNotifier) NotifyLoanCashPickupInitiated(ctx context.Context, n contracts.LoanNotification) error {
	msg := fmt.Sprintf(s.tmpl(ctx, n).CashPickupInitiated, n.LoanReference, n.InteractiveURL)
	return s.notifier.Send(ctx, n.PhoneNumber, msg)
}

func (s *SMSLoanNotifier) NotifyLoanCashPickupReady(ctx context.Context, n contracts.LoanNotification) error {
	msg := fmt.Sprintf(s.tmpl(ctx, n).CashPickupReady, n.DisplayCurrency, n.DisplayAmount, n.CashPickupRef, n.LoanReference)
	return s.notifier.Send(ctx, n.PhoneNumber, msg)
}

func (s *SMSLoanNotifier) NotifyRepaymentReminder(ctx context.Context, n contracts.LoanNotification) error {
	if n.DueDate == nil {
		return nil
	}

	daysUntilDue := int(time.Until(*n.DueDate).Hours() / 24)

	var msg string
	switch {
	case daysUntilDue <= 0:
		msg = fmt.Sprintf(s.tmpl(ctx, n).RepaymentOverdue,
			n.DisplayCurrency, n.DisplayAmount, n.LoanReference)
	case daysUntilDue <= 3:
		msg = fmt.Sprintf(s.tmpl(ctx, n).RepaymentSoon,
			n.DisplayCurrency, n.DisplayAmount, daysUntilDue, n.LoanReference)
	default:
		msg = fmt.Sprintf(s.tmpl(ctx, n).RepaymentUpcoming,
			n.DisplayCurrency, n.DisplayAmount, n.DueDate.Format("2006-01-02"), n.LoanReference)
	}

	return s.notifier.Send(ctx, n.PhoneNumber, msg)
}

// NoOpLoanNotifier discards all loan notifications silently.
type NoOpLoanNotifier struct{}

// Compile-time check.
var _ contracts.LoanNotifier = (*NoOpLoanNotifier)(nil)

func (*NoOpLoanNotifier) NotifyLoanApproved(context.Context, contracts.LoanNotification) error {
	return nil
}
func (*NoOpLoanNotifier) NotifyLoanRejected(context.Context, contracts.LoanNotification) error {
	return nil
}
func (*NoOpLoanNotifier) NotifyLoanDisbursed(context.Context, contracts.LoanNotification) error {
	return nil
}
func (*NoOpLoanNotifier) NotifyLoanFailed(context.Context, contracts.LoanNotification) error {
	return nil
}
func (*NoOpLoanNotifier) NotifyLoanOffRampFailed(context.Context, contracts.LoanNotification) error {
	return nil
}
func (*NoOpLoanNotifier) NotifyLoanCashPickupApproved(context.Context, contracts.LoanNotification) error {
	return nil
}
func (*NoOpLoanNotifier) NotifyRepaymentReceived(context.Context, contracts.LoanNotification) error {
	return nil
}
func (*NoOpLoanNotifier) NotifyRepaymentReminder(context.Context, contracts.LoanNotification) error {
	return nil
}
func (*NoOpLoanNotifier) NotifyLoanCashPickupInitiated(context.Context, contracts.LoanNotification) error {
	return nil
}
func (*NoOpLoanNotifier) NotifyLoanCashPickupReady(context.Context, contracts.LoanNotification) error {
	return nil
}
