package fonbnk

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// API paths.
const (
	pathQuote             = "/api/v2/quote"
	pathOrder             = "/api/v2/order"
	pathOrderConfirm      = "/api/v2/order/confirm"
	pathOrderCancel       = "/api/v2/order/cancel"
	pathOrderIntermediate = "/api/v2/order/intermediate-action"
	pathCurrencies        = "/api/v2/currencies"
	pathOrderLimits       = "/api/v2/order-limits"
)

// permissionGateMessage is what Fonbnk answers when the account lacks the
// create-users permission that every order endpoint needs.
const permissionGateMessage = "This feature is not available for this merchant"

// call performs one signed JSON request and decodes the response.
//
// endpoint must already carry its query string: the transport signs
// URL.Path plus URL.RawQuery, so the signed and sent strings must be the same
// bytes.
func call[T any](ctx context.Context, a *FonbnkAdapter, errb oops.OopsErrorBuilder, method, endpoint string, body any) (*T, error) {
	var reader io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, errb.Code(pkgErrors.CodeEncodeFailed).Wrapf(err, "could not encode the request")
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+endpoint, reader)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not build the request")
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeTransportFailed).Wrapf(err, "request did not complete")
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, parseError(errb, resp)
	}

	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, errb.Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the response")
	}
	return &out, nil
}

// parseError turns a non-2xx into a structured error, mapping the statuses
// Fonbnk overloads onto distinct codes.
func parseError(errb oops.OopsErrorBuilder, resp *http.Response) error {
	raw, _ := io.ReadAll(resp.Body)

	var apiErr APIError
	_ = json.Unmarshal(raw, &apiErr)
	message := firstNonEmpty(apiErr.Message, apiErr.Error, string(raw))

	errb = errb.
		With(pkgErrors.AttrStatusCode, resp.StatusCode).
		With("api_message", message)
	if apiErr.Code != "" {
		errb = errb.With("api_code", apiErr.Code)
	}

	switch {
	case resp.StatusCode == http.StatusForbidden && strings.Contains(message, permissionGateMessage):
		return errb.
			Code(pkgErrors.CodeMerchantNotPermitted).
			Hint("Ask Fonbnk support to enable the create-users permission on this merchant account.").
			Errorf("Fonbnk rejected the call: the merchant account lacks the required permission")
	case resp.StatusCode == http.StatusUnauthorized:
		return errb.Code(pkgErrors.CodeUnauthorized).Errorf("Fonbnk rejected the request signature")
	case resp.StatusCode == http.StatusNotFound:
		return errb.Code(pkgErrors.CodeNotFound).Errorf("Fonbnk has no such record")
	}
	return errb.Code(pkgErrors.CodeHTTPError).Errorf("Fonbnk returned a non-2xx")
}

func firstNonEmpty(values ...string) string {
	return lo.FindOrElse(values, "", func(v string) bool { return v != "" })
}

// directionOf reports OrderTypeOnRamp or OrderTypeOffRamp from a corridor's
// leg currency types, or "" when it is neither.
func directionOf(depositType, payoutType string) string {
	switch {
	case depositType == CurrencyTypeCrypto && payoutType == CurrencyTypeFiat:
		return OrderTypeOffRamp
	case depositType == CurrencyTypeFiat && payoutType == CurrencyTypeCrypto:
		return OrderTypeOnRamp
	}
	return ""
}

// withDirection tags an error with the corridor direction when it is
// derivable.
func withDirection(errb oops.OopsErrorBuilder, depositType, payoutType string) oops.OopsErrorBuilder {
	if direction := directionOf(depositType, payoutType); direction != "" {
		return errb.With(pkgErrors.AttrDirection, direction)
	}
	return errb
}

// withQuery appends an encoded query string, rendered once so the signed and
// sent bytes match.
func withQuery(path string, values url.Values) string {
	if len(values) == 0 {
		return path
	}
	return path + "?" + values.Encode()
}

// exactlyOneAmount enforces Fonbnk's rule that precisely one leg carries an
// amount. Checked here because the alternative is a 400 with no indication of
// which leg was at fault.
func exactlyOneAmount(errb oops.OopsErrorBuilder, deposit, payout *float64) error {
	switch {
	case deposit == nil && payout == nil:
		return errb.Code(pkgErrors.CodeMissingAmount).
			Errorf("neither leg carries an amount")
	case deposit != nil && payout != nil:
		return errb.Code(pkgErrors.CodeInvalidAmount).
			With("deposit_amount", *deposit).
			With("payout_amount", *payout).
			Errorf("both legs carry an amount")
	}
	return nil
}
