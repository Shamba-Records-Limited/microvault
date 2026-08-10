package moneygram

import "github.com/Shamba-Records-Limited/microvault/pkg/payment/offramp"

// Options carries MoneyGram-specific extras attached to offramp.Request.
// Required for cash-pickup off-ramps — the adapter rejects requests without
// it.
type Options struct {
	// FirstName is the user's first name, used for KYC verification.
	FirstName string

	// LastName is the user's last name, used for KYC verification.
	LastName string

	// BirthDate is an ISO-8601 (YYYY-MM-DD) string used for the SEP-9 KYC
	// prefill. MG honours this in the interactive webview.
	BirthDate string

	// MobileNumber is the user's mobile number, used for KYC verification.
	MobileNumber string

	// Address is the user's address, used for KYC verification.
	Address string

	// PostalCode is the user's postal code, used for KYC verification.
	PostalCode string

	// City is the user's city, used for KYC verification.
	City string

	// AddressCountryCode is the user's address country code, used for KYC verification.
	AddressCountryCode string

	// ChildAccountIndex is the per-user Stellar derivation index that drives
	// the SEP-10 child memo. Persist this on the loan row alongside the
	// resulting CashPickupPayload.ChildAccountMemo so pollers can re-derive
	// the memo on restart.
	ChildAccountIndex uint32
}

// ProviderID identifies this options payload's provider.
func (Options) ProviderID() offramp.ProviderID { return offramp.ProviderMoneyGram }

// CashPickupPayload is the offramp.Result.Provider value the MG adapter
// returns. Fields populated at initiation: InteractiveURL, ChildAccountMemo.
// The rest are backfilled by the poller as MG transitions the transaction
// through pending_user_transfer_complete and beyond.
type CashPickupPayload struct {
	InteractiveURL    string // SEP-24 webview URL — SMS to the user to start KYC
	ExternalReference string // cash-pickup reference number (populated post-completion)
	MoreInfoURL       string // MG-hosted support link for the transaction
	ChildAccountMemo  int64  // SEP-10 child memo for this withdrawal
	WithdrawMemo      string // memo MG expects on the treasury to anchor payment
	WithdrawMemoType  string // memo type ("id", "hash", "text") MG specifies
}

// ProviderID identifies this payload's provider.
func (CashPickupPayload) ProviderID() offramp.ProviderID { return offramp.ProviderMoneyGram }
