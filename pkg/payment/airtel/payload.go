package airtel

import "time"

// The types here are the provider-specific extras the cash-in contract will
// carry. They are plain structs: the ProviderID methods that satisfy
// cashin.ProviderOptions and cashin.ProviderPayload return a type from a
// package this one deliberately does not import yet, and are added in the
// task that crosses that boundary.

// Options carries Airtel-specific extras for a collection request.
type Options struct {
	// Reference is shown to the payer. Defaults to the loan reference.
	Reference string
}

// PromptPayload is what an accepted USSD push returns.
//
// An accepted prompt is not a payment. TransactionID identifies something the
// payer may still decline, mistype a PIN against, or let expire, and it is the
// handle for resolving which.
type PromptPayload struct {
	TransactionID string
	Status        TransactionStatus
	ResponseCode  string
	PromptedKES   int64
}

// CollectionPayload is what a settled collection produces.
type CollectionPayload struct {
	// AirtelMoneyID is Airtel's receipt. It exists only on success and is the
	// only key a refund accepts.
	AirtelMoneyID string

	// TransactionID is our own id, and the key an enquiry takes.
	TransactionID string

	MSISDN      MSISDN
	AmountMinor int64
	PaidAt      time.Time

	// HashVerified records whether the callback that disclosed this carried a
	// hash that verified. False on a collection observed by enquiry, where
	// there was no hash to check.
	HashVerified bool
}

// RefundPayload is what a refund result carries.
type RefundPayload struct {
	AirtelMoneyID string
	Status        TransactionStatus
}

// SettlementPayload is one entry from the reconciliation sweep.
type SettlementPayload struct {
	AirtelMoneyID   string
	TransactionID   string
	ReferenceNumber string
	AmountMinor     int64
	ChargeMinor     int64
	ServiceType     SummaryServiceType
	SettledAt       time.Time
}
