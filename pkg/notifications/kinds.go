package notifications

// Message kinds the portal looks up by name. Every loan template sends as
// "loan.<template>" and every account notice as "account.<event>".
const (
	KindLoanDisbursed     = "loan.disbursed"
	KindLoanRepaid        = "loan.repaid"
	KindRepaymentPaybill  = "loan.repayment_paybill"
	KindCashPickupReady   = "loan.cash_pickup_ready"
	KindRepaymentReminder = "loan.repayment_reminder"
	KindPilotInvite       = "account.pilot_invite"
)
