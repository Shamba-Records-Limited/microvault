package chipper

import (
	"strings"

	"github.com/govalues/decimal"
	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Amount is a request amount. It encodes as a JSON number, which the payout
// endpoint requires.
type Amount struct {
	decimal.Decimal
}

// NewAmount wraps d.
func NewAmount(d decimal.Decimal) *Amount { return &Amount{Decimal: d} }

// MarshalJSON encodes the amount as a bare JSON number.
func (a Amount) MarshalJSON() ([]byte, error) { return []byte(a.String()), nil }

// UnmarshalJSON accepts a JSON number or a numeric string.
func (a *Amount) UnmarshalJSON(data []byte) error { return a.Decimal.UnmarshalJSON(data) }

var currencyScales = map[string]int{
	"KES": 2, "GHS": 2, "NGN": 2, "ZMW": 2, "USD": 2,
	"UGX": 0, "TZS": 0, "RWF": 0,
	"USDC": 6, "USDT": 6,
}

// CurrencyScale reports the decimal places Chipper accepts for currency.
func CurrencyScale(currency string) (int, bool) {
	scale, ok := currencyScales[strings.ToUpper(currency)]
	return scale, ok
}

// checkAmount rejects a non-positive amount or one with more decimals than
// the currency allows. Unknown currencies are left to the API.
func checkAmount(errb oops.OopsErrorBuilder, field string, a *Amount, currency string) error {
	if a == nil {
		return nil
	}
	if !a.IsPos() {
		return errb.Code(pkgErrors.CodeInvalidAmount).With("field", field).With("amount", a.String()).
			Errorf("amount must be positive")
	}
	if scale, ok := CurrencyScale(currency); ok && a.Trim(0).Scale() > scale {
		return errb.Code(pkgErrors.CodeInvalidAmount).With("field", field).With("amount", a.String()).
			With(pkgErrors.AttrCurrency, currency).With("max_decimals", scale).
			Errorf("amount has more decimals than the currency allows")
	}
	return nil
}
