package contracts

import (
	"context"
	"errors"
)

// ErrVaultRepayDeferred is returned by RepayVault and RepayVaultAmount when the
// repay was not attempted: another caller holds it, its outcome is unknown, or
// it has exhausted its inline attempts and belongs to the reconciler.
var ErrVaultRepayDeferred = errors.New("vault repay deferred")

// DisbursementUpdater drives a loan's disbursement through its terminal states
// and the notifications and vault repayments that go with them. The YellowCard
// webhook service, its refund poller and the MoneyGram poller all depend on it;
// the lending module implements it once for every off-ramp.
type DisbursementUpdater interface {
	// UpdateDisbursementStatus sets the disbursement status of the loan
	// identified by sequenceID.
	UpdateDisbursementStatus(ctx context.Context, sequenceID, status string) error

	// RecordDisbursementCompletion persists the final financials of a
	// completed payment: the delivered local amount and the fees.
	RecordDisbursementCompletion(ctx context.Context, sequenceID string, fin CompletionFinancials) error

	// SetSettlementMethod updates the loan's settlement method. The refund
	// poller calls it when a direct-mode disbursement fails over to fiat, so
	// the eventual completion triggers the vault repay branch.
	SetSettlementMethod(ctx context.Context, sequenceID, method string) error

	// IsDirectSettlement reports whether the loan was disbursed via direct
	// settlement, which decides whether a failed payout waits for a refund.
	IsDirectSettlement(ctx context.Context, sequenceID string) (bool, error)

	// NotifyDisbursementComplete tells the borrower their disbursement
	// completed.
	NotifyDisbursementComplete(ctx context.Context, sequenceID string) error

	// NotifyDisbursementFailed tells the borrower their disbursement failed.
	NotifyDisbursementFailed(ctx context.Context, sequenceID string) error

	// NotifyCashPickupReady tells the borrower their cash is collectable and
	// quotes the MoneyGram reference number.
	NotifyCashPickupReady(ctx context.Context, sequenceID string) error

	// NotifyRefundReceived tells the borrower their cash pickup was cancelled
	// and the funds returned, and that they can request again.
	NotifyRefundReceived(ctx context.Context, sequenceID string) error

	// RepayVault returns the borrowed principal from treasury to the vault.
	// No-op if already repaid.
	RepayVault(ctx context.Context, sequenceID string) error

	// RepayVaultAmount repays an explicit stroop amount rather than the
	// principal. Used for refunds, where the anchor may return less than was
	// sent and repaying the principal would overdraw the treasury.
	RepayVaultAmount(ctx context.Context, sequenceID string, amountStroops int64) error
}

// CompletionFinancials carries the final amounts of a completed off-ramp
// payment, looked up at completion time.
type CompletionFinancials struct {
	ConvertedAmountLocal  float64
	ServiceFeeAmountUSD   float64
	ServiceFeeAmountLocal float64
	PartnerFeeAmountUSD   float64
	PartnerFeeAmountLocal float64
}
