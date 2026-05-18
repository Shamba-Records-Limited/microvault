package yellowcard

import "github.com/Shamba-Records-Limited/microvault/pkg/payment/offramp"

// Options carries YellowCard-specific extras attached to offramp.Request.
// Zero-value Options is treated as direct settlement.
type Options struct {
	// SettlementMethod selects direct (crypto-funded) vs fiat (balance-funded)
	// disbursement. Use SettlementMethodDirect / SettlementMethodFiat.
	// Empty defaults to direct.
	SettlementMethod string
}

// ProviderID identifies this options payload's provider.
func (Options) ProviderID() offramp.ProviderID { return offramp.ProviderYellowCard }

// DirectSettlementPayload is the offramp.Result.Provider value the YC adapter
// returns when settlement mode is direct: the treasury sends USDC to the YC-
// issued Stellar wallet, then YC disburses fiat.
type DirectSettlementPayload struct {
	StellarAddress string // YC-issued wallet
	StellarMemo    string // memo/walletTag accompanying the USDC transfer
	StellarTxHash  string // hash of the treasury → YC payment
}

// ProviderID identifies this payload's provider.
func (DirectSettlementPayload) ProviderID() offramp.ProviderID { return offramp.ProviderYellowCard }

// FiatPayload is the offramp.Result.Provider value returned when YC disburses
// directly from its pre-funded balance — no Stellar transfer originates from
// the treasury, so the struct is intentionally empty.
type FiatPayload struct{}

// ProviderID identifies this payload's provider.
func (FiatPayload) ProviderID() offramp.ProviderID { return offramp.ProviderYellowCard }
