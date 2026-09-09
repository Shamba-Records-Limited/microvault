package mpesa

import (
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/cashin"
)

// The types here are the provider-specific extras the cashin contract carries.
// Each satisfies the ProviderOptions or ProviderPayload marker.

// Options carries M-Pesa-specific extras for a collection request.
type Options struct {
	TransactionType TransactionType
	Shortcode       uint

	// TransactionDesc is shown in the prompt. Thirteen characters at most.
	TransactionDesc string
}

func (Options) ProviderID() cashin.ProviderID { return cashin.ProviderMpesa }

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

func (ExpressPayload) ProviderID() cashin.ProviderID { return cashin.ProviderMpesa }

// PayBillPayload is the instruction set for a passive paybill collection.
type PayBillPayload struct {
	Shortcode        uint
	AccountReference string
}

func (PayBillPayload) ProviderID() cashin.ProviderID { return cashin.ProviderMpesa }

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

func (C2BPayload) ProviderID() cashin.ProviderID { return cashin.ProviderMpesa }

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

func (ReversalPayload) ProviderID() cashin.ProviderID { return cashin.ProviderMpesa }

// Compile-time satisfaction of the contract markers.
var (
	_ cashin.ProviderOptions = Options{}
	_ cashin.ProviderPayload = ExpressPayload{}
	_ cashin.ProviderPayload = C2BPayload{}
	_ cashin.ProviderPayload = ReversalPayload{}
	_ cashin.ProviderPayload = PayBillPayload{}
)
