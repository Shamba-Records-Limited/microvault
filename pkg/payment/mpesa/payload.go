package mpesa

import "time"

// The types here are the provider-specific extras a cash-in contract would
// carry. They are plain structs with no marker method, because the contract
// package that would define the marker interface does not exist yet. Satisfying
// it later is one method per type and no change to any field.

// Options carries M-Pesa-specific extras for a collection request.
type Options struct {
	TransactionType TransactionType
	Shortcode       uint

	// TransactionDesc is shown in the prompt. Thirteen characters at most.
	TransactionDesc string
}

// ExpressPayload is what an M-Pesa Express collection returns.
//
// A prompt that was accepted is not a payment. CheckoutRequestID identifies
// something that may still be cancelled, time out, or be declined for a wrong
// PIN, and it is the handle for resolving which.
type ExpressPayload struct {
	MerchantRequestID string
	CheckoutRequestID string
	CustomerMessage   string
	PromptedAmountKES int64
}

// C2BPayload is what a paybill collection produces.
type C2BPayload struct {
	// TransID is the M-Pesa receipt and the natural idempotency key: a unique
	// index on it makes a duplicate confirmation a no-op rather than a second
	// credit.
	TransID string

	// BillRefNumber is the reference the payer typed. On a shared paybill it
	// is the only thing binding the payment to a loan.
	BillRefNumber string

	// MaskedMSISDN is all a confirmation discloses. The unmasked number comes
	// from Pull Transaction.
	MaskedMSISDN MaskedMSISDN

	ThirdPartyTransID string
	AmountMinor       int64
	PaidAt            time.Time
}

// ReversalPayload is what a reversal result carries.
type ReversalPayload struct {
	TransactionID         string
	OriginalTransactionID string
	AmountMinor           int64
	ChargeMinor           int64

	// CreditPartyPublicName arrives unmasked and with a full name. It belongs
	// in an audit record, never in a log line.
	CreditPartyPublicName string
}
