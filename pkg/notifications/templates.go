package notifications

// LoanTemplates contains format strings for each loan lifecycle notification.
// Each template uses fmt.Sprintf-style verbs; the exact arguments are
// documented per-field.
type LoanTemplates struct {
	// Approved: args = (DisplayCurrency, DisplayAmount, LoanReference)
	Approved string
	// Rejected: args = (DisplayCurrency, DisplayAmount, Reason)
	Rejected string
	// Disbursed: args = (DisplayCurrency, DisplayAmount, LoanReference)
	Disbursed string
	// Failed: args = (LoanReference)
	Failed string
	// OffRampFailed: args = (DisplayCurrency, DisplayAmount, LoanReference).
	// Sent when vault borrow succeeded but the off-ramp could not be
	// initiated/completed and the USDC has been returned to the vault. The
	// borrower owes nothing — distinct from the credit-default "Failed".
	OffRampFailed string
	// CashPickupApproved: args = (DisplayCurrency, DisplayAmount, LoanReference).
	// Sent when a cash-pickup loan is approved, in place of "Approved" — the
	// generic copy implies a push disbursement, which is misleading here. A
	// second SMS with the MoneyGram interactive URL follows once the off-ramp
	// is initiated (see CashPickupInitiated).
	CashPickupApproved string
	// RepaymentReceived: args = (DisplayCurrency, DisplayAmount, LoanReference, DisplayCurrency, RemainingBalance)
	RepaymentReceived string
	// RepaymentOverdue: args = (DisplayCurrency, DisplayAmount, LoanReference)
	RepaymentOverdue string
	// RepaymentSoon: args = (DisplayCurrency, DisplayAmount, daysUntilDue, LoanReference)
	RepaymentSoon string
	// RepaymentUpcoming: args = (DisplayCurrency, DisplayAmount, dueDateFormatted, LoanReference)
	RepaymentUpcoming string
	// CashPickupInitiated: args = (LoanReference, InteractiveURL)
	CashPickupInitiated string
	// CashPickupReady: args = (DisplayCurrency, DisplayAmount, CashPickupRef, LoanReference)
	CashPickupReady string
}

// DefaultLoanTemplates returns ready-to-use loan templates, parameterized for
// any currency.
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
		OffRampFailed: "We could not disburse your loan of %s %.2f (Ref: %s). " +
			"No funds left your account and you owe nothing. Please try again or contact support.",
		CashPickupApproved: "Your cash-pickup loan of %s %.2f has been approved (Ref: %s). " +
			"You will receive a verification link shortly to complete pickup at a MoneyGram agent.",
		RepaymentReceived: "Payment of %s %.2f received for loan %s. " +
			"Remaining balance: %s %.2f. Thank you!",
		RepaymentOverdue: "URGENT: Your loan payment of %s %.2f is overdue (Ref: %s). " +
			"Please pay immediately to avoid penalties.",
		RepaymentSoon: "Reminder: Your loan payment of %s %.2f is due in %d days (Ref: %s). " +
			"Dial *384*1234# to pay.",
		RepaymentUpcoming: "Reminder: Your loan payment of %s %.2f is due on %s (Ref: %s).",
		CashPickupInitiated: "Cash-pickup loan (Ref: %s) ready.\n\n" +
			"Open to verify:\n\n%s\n\n" +
			"Final amount shown in the link.",
		CashPickupReady: "Your loan of %s %.2f is ready for pickup at any MoneyGram agent. " +
			"Reference: %s (Loan: %s). Bring valid ID.",
	}
}
