package moneygram

// DefaultOriginatingCountry is the send-side country for FX rate lookups.
// MoneyGram documents USA as the originating corridor for the FX Rate
// endpoint, and the treasury settles in USDC regardless of borrower location.
const DefaultOriginatingCountry = "USA"

// DefaultSendCurrency is what the treasury holds. USDC tracks USD 1:1 for
// quoting purposes.
const DefaultSendCurrency = "USD"

// Withdraw bounds published by MoneyGram's SEP-24 /info, in USD. The floor
// rose from 1 to 15 with the 2026-08-01 anchor migration; see
// internal-docs/moneygram-integration.md Appendix D.
//
// Nothing reads /info at runtime, so these are the single source of truth for
// the limit across the USSD gate and the adapter's advertised provider info.
const (
	MinWithdrawUSD = 15.0
	MaxWithdrawUSD = 2500.0
)

// currencyToCountry maps a payout currency to the ISO-3 country MoneyGram
// quotes it under. Limited to corridors microvault serves; an unmapped
// currency yields "", which callers use to skip MoneyGram's leg rather than
// quote against a guessed corridor.
var currencyToCountry = map[string]string{
	"KES": "KEN",
	"UGX": "UGA",
	"TZS": "TZA",
	"RWF": "RWA",
	"NGN": "NGA",
	"GHS": "GHA",
	"ZAR": "ZAF",
}

// CountryISO3ForCurrency returns the ISO-3 country MoneyGram quotes currency
// under, or "" when the corridor is not mapped.
func CountryISO3ForCurrency(currency string) string {
	return currencyToCountry[currency]
}
