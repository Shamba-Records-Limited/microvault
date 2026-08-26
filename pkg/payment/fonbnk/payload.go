package fonbnk

import "github.com/Shamba-Records-Limited/microvault/pkg/payment/offramp"

// Options carries Fonbnk-specific extras attached to offramp.Request.
type Options struct {
	UserEmail string
	UserIP    string

	// CarrierCode selects the mobile-money operator, e.g. ke_safaricom.
	CarrierCode string

	// Quote, when set, prices the order at an already-locked rate. The relay
	// produces one; without it Fonbnk prices at creation time.
	Quote *Quote

	// SandboxForcedFlow forces a deposit outcome in sandbox. Ignored in
	// production.
	SandboxForcedFlow string
}

// ProviderID identifies this options payload's provider.
func (Options) ProviderID() offramp.ProviderID { return offramp.ProviderFonbnk }

// OffRampPayload is the offramp.Result.Provider value the Fonbnk adapter
// returns. The treasury sends USDC to StellarAddress, then Fonbnk pays out
// fiat.
type OffRampPayload struct {
	OrderID        string
	OrderParams    string
	StellarAddress string
	StellarMemo    string
	StellarTxHash  string

	// Confirmed reports whether the deposit was acknowledged to Fonbnk. False
	// means the USDC was sent but the confirm call failed; Fonbnk detects
	// incoming payments on its own, so the order still settles, but it is
	// worth an operator's attention.
	Confirmed bool
}

// ProviderID identifies this payload's provider.
func (OffRampPayload) ProviderID() offramp.ProviderID { return offramp.ProviderFonbnk }
