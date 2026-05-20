package notifications

import (
	"context"
	"fmt"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/contracts"
)

// SMSLoanNotifier implements contracts.LoanNotifier using a Notifier transport
// and configurable LoanTemplates.
type SMSLoanNotifier struct {
	notifier  Notifier
	templates *LoanTemplates
}

// Compile-time check.
var _ contracts.LoanNotifier = (*SMSLoanNotifier)(nil)

// NewSMSLoanNotifier creates a new SMSLoanNotifier. Pass nil for templates to
// use DefaultLoanTemplates().
func NewSMSLoanNotifier(notifier Notifier, templates *LoanTemplates) *SMSLoanNotifier {
	if templates == nil {
		templates = DefaultLoanTemplates()
	}
	return &SMSLoanNotifier{notifier: notifier, templates: templates}
}

func (s *SMSLoanNotifier) NotifyLoanApproved(ctx context.Context, n contracts.LoanNotification) error {
	msg := fmt.Sprintf(s.templates.Approved, n.DisplayCurrency, n.DisplayAmount, n.LoanNumber)
	return s.notifier.Send(ctx, n.PhoneNumber, msg)
}

func (s *SMSLoanNotifier) NotifyLoanRejected(ctx context.Context, n contracts.LoanNotification) error {
	msg := fmt.Sprintf(s.templates.Rejected, n.DisplayCurrency, n.DisplayAmount, n.Reason)
	return s.notifier.Send(ctx, n.PhoneNumber, msg)
}

func (s *SMSLoanNotifier) NotifyLoanDisbursed(ctx context.Context, n contracts.LoanNotification) error {
	msg := fmt.Sprintf(s.templates.Disbursed, n.DisplayCurrency, n.DisplayAmount, n.LoanNumber)
	return s.notifier.Send(ctx, n.PhoneNumber, msg)
}

func (s *SMSLoanNotifier) NotifyLoanFailed(ctx context.Context, n contracts.LoanNotification) error {
	msg := fmt.Sprintf(s.templates.Failed, n.LoanNumber)
	return s.notifier.Send(ctx, n.PhoneNumber, msg)
}

func (s *SMSLoanNotifier) NotifyLoanOffRampFailed(ctx context.Context, n contracts.LoanNotification) error {
	msg := fmt.Sprintf(s.templates.OffRampFailed, n.DisplayCurrency, n.DisplayAmount, n.LoanNumber)
	return s.notifier.Send(ctx, n.PhoneNumber, msg)
}

func (s *SMSLoanNotifier) NotifyLoanCashPickupApproved(ctx context.Context, n contracts.LoanNotification) error {
	msg := fmt.Sprintf(s.templates.CashPickupApproved, n.DisplayCurrency, n.DisplayAmount, n.LoanNumber)
	return s.notifier.Send(ctx, n.PhoneNumber, msg)
}

func (s *SMSLoanNotifier) NotifyRepaymentReceived(ctx context.Context, n contracts.LoanNotification) error {
	msg := fmt.Sprintf(s.templates.RepaymentReceived,
		n.DisplayCurrency, n.DisplayAmount, n.LoanNumber,
		n.DisplayCurrency, n.RemainingBalance)
	return s.notifier.Send(ctx, n.PhoneNumber, msg)
}

func (s *SMSLoanNotifier) NotifyLoanCashPickupInitiated(ctx context.Context, n contracts.LoanNotification) error {
	msg := fmt.Sprintf(s.templates.CashPickupInitiated, n.LoanNumber, n.InteractiveURL)
	return s.notifier.Send(ctx, n.PhoneNumber, msg)
}

func (s *SMSLoanNotifier) NotifyLoanCashPickupReady(ctx context.Context, n contracts.LoanNotification) error {
	msg := fmt.Sprintf(s.templates.CashPickupReady, n.DisplayCurrency, n.DisplayAmount, n.CashPickupRef, n.LoanNumber)
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
		msg = fmt.Sprintf(s.templates.RepaymentOverdue,
			n.DisplayCurrency, n.DisplayAmount, n.LoanNumber)
	case daysUntilDue <= 3:
		msg = fmt.Sprintf(s.templates.RepaymentSoon,
			n.DisplayCurrency, n.DisplayAmount, daysUntilDue, n.LoanNumber)
	default:
		msg = fmt.Sprintf(s.templates.RepaymentUpcoming,
			n.DisplayCurrency, n.DisplayAmount, n.DueDate.Format("2006-01-02"), n.LoanNumber)
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
