package contracts

import (
	"context"
	"time"
)

// Lender defines the interface for loan origination and retrieval.
type Lender interface {
	AssessEligibility(ctx context.Context, req EligibilityRequest) (*EligibilityResult, error)
	CreateLoan(ctx context.Context, req CreateLoanRequest) (*LoanRecord, error)
	GetLoan(ctx context.Context, loanID string) (*LoanRecord, error)
	GetUserLoans(ctx context.Context, userID string) ([]*LoanRecord, error)
}

// LoanNotifier defines the interface for sending loan lifecycle notifications.
type LoanNotifier interface {
	NotifyLoanApproved(ctx context.Context, n LoanNotification) error
	NotifyLoanRejected(ctx context.Context, n LoanNotification) error
	NotifyLoanDisbursed(ctx context.Context, n LoanNotification) error
	NotifyLoanFailed(ctx context.Context, n LoanNotification) error
	// NotifyLoanOffRampFailed reports that vault borrow succeeded but fiat
	// disbursement could not be completed and the USDC has been returned to
	// the vault. Distinct from NotifyLoanFailed, which is a credit-default
	// notice.
	NotifyLoanOffRampFailed(ctx context.Context, n LoanNotification) error
	// NotifyLoanCashPickupApproved acknowledges approval of a cash-pickup
	// loan without implying a push disbursement is on the way. A subsequent
	// NotifyLoanCashPickupInitiated carries the MoneyGram interactive URL.
	NotifyLoanCashPickupApproved(ctx context.Context, n LoanNotification) error
	NotifyRepaymentReceived(ctx context.Context, n LoanNotification) error
	NotifyRepaymentReminder(ctx context.Context, n LoanNotification) error

	// NotifyLoanStatement sends the borrower a record of one loan —
	// reference, amount and due date. Unlike the other methods this is
	// user-initiated rather than lifecycle-driven: it answers a "My Loans"
	// request, so it is sent on demand and repeats are expected.
	NotifyLoanStatement(ctx context.Context, n LoanNotification) error

	// NotifyLoanCashPickupInitiated sends the MoneyGram interactive URL to
	// the user. The locked payout amount is unknown at this point and
	// confirmed when the user opens the link; the template should make that
	// disclosure explicit.
	NotifyLoanCashPickupInitiated(ctx context.Context, n LoanNotification) error

	// NotifyLoanCashPickupReady sends the cash-pickup reference number once
	// MoneyGram has locked the payout. Template should include the
	// reference, the locked amount, and the currency.
	NotifyLoanCashPickupReady(ctx context.Context, n LoanNotification) error

	// NotifyLoanCashPickupCancelled tells the borrower their cash pickup was
	// cancelled and the funds returned. Distinct from NotifyLoanOffRampFailed:
	// the usual cause is the borrower cancelling in MoneyGram's own app, so
	// this reads as an acknowledgement rather than a failure.
	NotifyLoanCashPickupCancelled(ctx context.Context, n LoanNotification) error
}

// EligibilityRequest contains the data needed to assess loan eligibility.
type EligibilityRequest struct {
	UserID       string
	Amount       int64 // In stroops (USDC * 10^7)
	DurationDays int
}

// EligibilityResult contains the outcome of an eligibility assessment.
type EligibilityResult struct {
	Approved     bool
	Reason       string
	MaxAmount    int64   // Maximum eligible amount in stroops
	InterestRate float64 // Annual interest rate as a decimal (e.g. 0.12 = 12%)
}

// CreateLoanRequest contains the data needed to create a new loan.
type CreateLoanRequest struct {
	UserID            string
	AccountID         string
	PrincipalAmount   int64  // In stroops
	PrincipalAsset    string // e.g. "USDC"
	InterestRateBps   int32  // Basis points
	DurationDays      int
	RepaymentSchedule string // "daily", "weekly", "bi_weekly", "monthly", "lump_sum"
}

// LoanRecord represents a loan with vault and ramp disbursement tracking.
type LoanRecord struct {
	ID                 string
	LoanReference      string
	UserID             string
	AccountID          string
	PrincipalAmount    int64
	PrincipalAsset     string
	InterestRateBps    int32
	TotalAmount        int64
	DurationDays       int
	RepaymentSchedule  string
	DueDate            *time.Time
	Status             string // "pending", "approved", "disbursed", "repaid", "defaulted", "cancelled"
	VaultTxHash        string
	RampProvider       string
	RampRequestID      string
	RampSequenceID     string
	RampFiatAmount     int64
	RampFiatCurrency   string
	SettlementMethod   string // "direct" or "fiat"
	DisbursementStatus string // "pending", "crypto_sent", "processing", "complete", "refund_pending", "failed"
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// LoanNotification contains data for loan lifecycle notifications.
type LoanNotification struct {
	LoanID           string
	LoanReference    string
	UserID           string
	PhoneNumber      string
	Amount           int64      // Raw amount (stroops/cents) for consumers that need it
	DisplayAmount    float64    // Display-ready (e.g. 5000.00)
	DisplayCurrency  string     // e.g. "KES", "USD"
	Status           string
	Reason           string     // For rejection notifications
	RemainingBalance float64    // For repayment confirmations
	DueDate          *time.Time // For repayment reminders

	// InteractiveURL is the MoneyGram SEP-24 webview URL sent in the
	// cash-pickup initiated SMS. Empty for non-MG notifications.
	InteractiveURL string

	// CashPickupRef is the reference number the user quotes at the MG agent
	// when collecting cash. Populated once MG locks the payout.
	CashPickupRef string

	// CashPickupInfoURL is MoneyGram's support deep-link for the transaction,
	// sent alongside the reference so a borrower with a problem at the agent
	// has somewhere to go. Unlike InteractiveURL it stays valid after the
	// withdrawal settles.
	CashPickupInfoURL string

	// Language optionally pins the SMS language (ISO code: en/sw/fr). When
	// empty the notifier resolves it from the recipient's stored preference.
	Language string
}
