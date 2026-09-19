package airtel

import (
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/cashin"
)

// The types here are the provider-specific extras the cash-in contract
// carries. Each satisfies the ProviderOptions or ProviderPayload marker.

// Options carries Airtel-specific extras for a collection request.
//
// Naming this provider in a request is how the Airtel rail is reached: the
// registry's method aliases point at M-Pesa, because a borrower picks their
// network from the repay menu rather than having it guessed from their
// number.
type Options struct {
	// Reference is shown to the payer. Defaults to the loan reference.
	Reference string
}

func (Options) ProviderID() cashin.ProviderID { return cashin.ProviderAirtel }

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

func (PromptPayload) ProviderID() cashin.ProviderID { return cashin.ProviderAirtel }

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

func (CollectionPayload) ProviderID() cashin.ProviderID { return cashin.ProviderAirtel }

// RefundPayload is what a refund result carries.
type RefundPayload struct {
	AirtelMoneyID string
	Status        TransactionStatus
}

func (RefundPayload) ProviderID() cashin.ProviderID { return cashin.ProviderAirtel }

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

func (SettlementPayload) ProviderID() cashin.ProviderID { return cashin.ProviderAirtel }

// Compile-time satisfaction of the contract markers.
var (
	_ cashin.ProviderOptions = Options{}
	_ cashin.ProviderPayload = PromptPayload{}
	_ cashin.ProviderPayload = CollectionPayload{}
	_ cashin.ProviderPayload = RefundPayload{}
	_ cashin.ProviderPayload = SettlementPayload{}
)
