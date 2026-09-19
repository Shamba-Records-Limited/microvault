package airtel

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

const pathBalanceEnquiry = "/standard/v2/users/balance"

var epBalanceEnquiry = endpoint{method: http.MethodGet, path: pathBalanceEnquiry, headers: headerUpper}

// BalanceResponse is the wallet balance.
type BalanceResponse struct {
	Data struct {
		// Balance is a formatted string — "37,600.00" — not a number. Airtel
		// documents it that way and the samples confirm it, so it is decoded
		// as a string and parsed explicitly rather than coerced.
		Balance string `json:"balance"`

		Currency      string `json:"currency"`
		AccountStatus string `json:"account_status"`
	} `json:"data"`
	Status Status `json:"status"`
}

func (r *BalanceResponse) envelope() Status { return r.Status }

// Minor returns the balance in minor units.
//
// It fails rather than guessing. A balance that cannot be parsed is not zero,
// and a float-alert that silently reads a malformed balance as zero would page
// an operator about a shortfall that does not exist.
func (r BalanceResponse) Minor() (int64, error) {
	return ParseFormattedAmount(r.Data.Balance)
}

// ParseFormattedAmount converts Airtel's grouped decimal rendering to minor
// units.
func ParseFormattedAmount(value string) (int64, error) {
	errb := airtelErr("parse_amount").With("value", value)

	cleaned := strings.ReplaceAll(strings.TrimSpace(value), ",", "")
	cleaned = strings.ReplaceAll(cleaned, " ", "")
	if cleaned == "" {
		return 0, errb.Code(pkgErrors.CodeDecodeFailed).Errorf("the amount is empty")
	}

	parsed, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return 0, errb.Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "the amount is not a number")
	}
	return int64(math.Round(parsed * 100)), nil
}

// BalanceEnquiry reads the configured wallet's balance.
//
// Airtel's parameter table lists a type parameter with values DISB, COLL,
// CASHIN and CASHOUT and labels it a path parameter, but the documented path
// has no placeholder for it and the curl sample omits it entirely. It is left
// unsent until the staging suite establishes what it actually is.
func (c *Client) BalanceEnquiry(ctx context.Context) (*BalanceResponse, error) {
	errb := airtelErr("balance_enquiry")
	return call[BalanceResponse](ctx, c, errb, epBalanceEnquiry, epBalanceEnquiry.path, nil)
}
