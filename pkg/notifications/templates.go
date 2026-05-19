package notifications

// LoanTemplates contains format strings for each loan lifecycle notification.
// Each template uses fmt.Sprintf-style verbs; the exact arguments are
// documented per-field.
type LoanTemplates struct {
	// Approved: args = (DisplayCurrency, DisplayAmount, LoanNumber)
	Approved string
	// Rejected: args = (DisplayCurrency, DisplayAmount, Reason)
	Rejected string
	// Disbursed: args = (DisplayCurrency, DisplayAmount, LoanNumber)
	Disbursed string
	// Failed: args = (LoanNumber)
	Failed string
	// RepaymentReceived: args = (DisplayCurrency, DisplayAmount, LoanNumber, DisplayCurrency, RemainingBalance)
	RepaymentReceived string
	// RepaymentOverdue: args = (DisplayCurrency, DisplayAmount, LoanNumber)
	RepaymentOverdue string
	// RepaymentSoon: args = (DisplayCurrency, DisplayAmount, daysUntilDue, LoanNumber)
	RepaymentSoon string
	// RepaymentUpcoming: args = (DisplayCurrency, DisplayAmount, dueDateFormatted, LoanNumber)
	RepaymentUpcoming string
	// CashPickupInitiated: args = (LoanNumber, InteractiveURL)
	CashPickupInitiated string
	// CashPickupReady: args = (DisplayCurrency, DisplayAmount, CashPickupRef, LoanNumber)
	CashPickupReady string
}

// DefaultLoanTemplates returns templates matching the current microvault-credit
// messages but parameterized for any currency.
func DefaultLoanTemplates() *LoanTemplates {
	return &LoanTemplates{
		Approved: "Congratulations! Your loan of %s %.2f has been approved (Ref: %s). " +
			"We are processing it and you will be notified when it's disbursed.",
		Rejected: "Your loan request for %s %.2f was not approved. Reason: %s. " +
			"Dial *384*1234# for more info.",
		Disbursed: "Your loan of %s %.2f has been disbursed (Ref: %s). " +
			"The funds are now available in your account.",
		Failed: "Your loan (Ref: %s) has been marked as defaulted. " +
			"This will affect your credit score. Please contact support.",
		RepaymentReceived: "Payment of %s %.2f received for loan %s. " +
			"Remaining balance: %s %.2f. Thank you!",
		RepaymentOverdue: "URGENT: Your loan payment of %s %.2f is overdue (Ref: %s). " +
			"Please pay immediately to avoid penalties.",
		RepaymentSoon: "Reminder: Your loan payment of %s %.2f is due in %d days (Ref: %s). " +
			"Dial *384*1234# to pay.",
		RepaymentUpcoming: "Reminder: Your loan payment of %s %.2f is due on %s (Ref: %s).",
		CashPickupInitiated: "Your cash-pickup loan (Ref: %s) is ready to confirm. " +
			"Open %s to verify your details. Final amount is confirmed when you open the link.",
		CashPickupReady: "Your loan of %s %.2f is ready for pickup at any MoneyGram agent. " +
			"Reference: %s (Loan: %s). Bring valid ID.",
	}
}
