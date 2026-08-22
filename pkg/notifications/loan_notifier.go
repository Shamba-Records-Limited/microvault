package notifications

import (
	"context"
	"fmt"

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

// NewSMSLoanNotifier creates a new SMSLoanNotifier over the built-in localized
// templates (en/sw/fr), adjusted by any options.
//
// Every merged template is rendered against [SentinelLoanNotification] before
// the notifier is returned, so an unset or non-GSM-7 message fails here rather
// than on a borrower's handset.
func NewSMSLoanNotifier(notifier Notifier, opts ...LoanOption) (*SMSLoanNotifier, error) {
	var cfg loanConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	set := localizedLoanTemplates()
	for lang, override := range cfg.overrides {
		base, ok := set[lang]
		if !ok {
			return nil, fmt.Errorf("loan templates: unsupported language %q", lang)
		}
		set[lang] = mergeInto(base, override)
	}
	for lang, merged := range set {
		if err := validateLoanTemplates(lang, merged); err != nil {
			return nil, err
		}
	}

	return &SMSLoanNotifier{notifier: notifier, templates: set, resolveLang: cfg.resolve}, nil
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
	return s.notifier.Send(ctx, n.PhoneNumber, s.tmpl(ctx, n).Approved(n))
}

func (s *SMSLoanNotifier) NotifyLoanRejected(ctx context.Context, n contracts.LoanNotification) error {
	return s.notifier.Send(ctx, n.PhoneNumber, s.tmpl(ctx, n).Rejected(n))
}

func (s *SMSLoanNotifier) NotifyLoanDisbursed(ctx context.Context, n contracts.LoanNotification) error {
	return s.notifier.Send(ctx, n.PhoneNumber, s.tmpl(ctx, n).Disbursed(n))
}

func (s *SMSLoanNotifier) NotifyLoanFailed(ctx context.Context, n contracts.LoanNotification) error {
	return s.notifier.Send(ctx, n.PhoneNumber, s.tmpl(ctx, n).Failed(n))
}

func (s *SMSLoanNotifier) NotifyLoanOffRampFailed(ctx context.Context, n contracts.LoanNotification) error {
	return s.notifier.Send(ctx, n.PhoneNumber, s.tmpl(ctx, n).OffRampFailed(n))
}

func (s *SMSLoanNotifier) NotifyLoanCashPickupApproved(ctx context.Context, n contracts.LoanNotification) error {
	return s.notifier.Send(ctx, n.PhoneNumber, s.tmpl(ctx, n).CashPickupApproved(n))
}

func (s *SMSLoanNotifier) NotifyRepaymentReceived(ctx context.Context, n contracts.LoanNotification) error {
	return s.notifier.Send(ctx, n.PhoneNumber, s.tmpl(ctx, n).RepaymentReceived(n))
}

func (s *SMSLoanNotifier) NotifyLoanCashPickupInitiated(ctx context.Context, n contracts.LoanNotification) error {
	return s.notifier.Send(ctx, n.PhoneNumber, s.tmpl(ctx, n).CashPickupInitiated(n))
}

func (s *SMSLoanNotifier) NotifyLoanCashPickupReady(ctx context.Context, n contracts.LoanNotification) error {
	return s.notifier.Send(ctx, n.PhoneNumber, s.tmpl(ctx, n).CashPickupReady(n))
}

func (s *SMSLoanNotifier) NotifyLoanCashPickupCancelled(ctx context.Context, n contracts.LoanNotification) error {
	return s.notifier.Send(ctx, n.PhoneNumber, s.tmpl(ctx, n).CashPickupCancelled(n))
}

func (s *SMSLoanNotifier) NotifyRepaymentReminder(ctx context.Context, n contracts.LoanNotification) error {
	if n.DueDate == nil {
		return nil
	}

	t := s.tmpl(ctx, n)
	var msg string
	switch days := DaysUntilDue(n); {
	case days <= 0:
		msg = t.RepaymentOverdue(n)
	case days <= 3:
		msg = t.RepaymentSoon(n)
	default:
		msg = t.RepaymentUpcoming(n)
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
func (*NoOpLoanNotifier) NotifyLoanCashPickupCancelled(context.Context, contracts.LoanNotification) error {
	return nil
}
