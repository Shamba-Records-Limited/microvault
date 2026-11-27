package chipper

import "github.com/Shamba-Records-Limited/microvault/pkg/payment/offramp"

// Settlement methods.
const (
	SettlementDirect = "direct"
	SettlementFiat   = "fiat"
)

// Options carries Chipper extras on offramp.Request. Empty SettlementMethod
// means direct.
type Options struct {
	SettlementMethod string
}

// ProviderID identifies this options payload's provider.
func (Options) ProviderID() offramp.ProviderID { return offramp.ProviderChipper }

// OffRampPayload is the offramp.Result.Provider value of the Chipper adapter.
// PayoutPending means the USDC reached Chipper but no payout exists yet.
type OffRampPayload struct {
	MethodCode          string
	PayoutID            string
	DepositTxHash       string
	DepositCollectionID string
	PayoutPending       bool
	PendingReason       string
}

// ProviderID identifies this payload's provider.
func (OffRampPayload) ProviderID() offramp.ProviderID { return offramp.ProviderChipper }
