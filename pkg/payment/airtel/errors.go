package airtel

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// AirtelError is a rejection carrying the full status envelope. It is wrapped
// by the oops error the package returns, so callers can reach it with
// errors.As to inspect the exact response code rather than the coarse
// pkgErrors code.
type AirtelError struct {
	StatusCode int
	Status     Status
}

func (e *AirtelError) Error() string {
	code := lo.CoalesceOrEmpty(e.Status.ResponseCode, e.Status.ResultCode)
	message := lo.CoalesceOrEmpty(e.Status.Message, "no message")
	if code == "" {
		return fmt.Sprintf("airtel returned %d: %s", e.StatusCode, message)
	}
	return fmt.Sprintf("airtel %s: %s", code, message)
}

// ResponseCode reports the product-specific code, falling back to the legacy
// result code when the product has not been migrated.
func (e *AirtelError) ResponseCode() string {
	return lo.CoalesceOrEmpty(e.Status.ResponseCode, e.Status.ResultCode)
}

// Collection error family, DP00800…
const (
	CodeCollectionAmbiguous    = "DP00800001000"
	CodeCollectionSuccess      = "DP00800001001"
	CodeCollectionIncorrectPin = "DP00800001002"
	CodeCollectionLimit        = "DP00800001003"
	CodeCollectionInvalidAmt   = "DP00800001004"
	CodeCollectionNoPinEntered = "DP00800001005"
	CodeCollectionInProcess    = "DP00800001006"
	CodeCollectionNoBalance    = "DP00800001007"
	CodeCollectionRefused      = "DP00800001008"
	CodeCollectionDoNotHonor   = "DP00800001009"
	CodeCollectionPayeeBarred  = "DP00800001010"
	CodeCollectionTimedOut     = "DP00800001024"
	CodeCollectionNotFound     = "DP00800001025"
	CodeCollectionForbidden    = "DP00800001026"
	CodeCollectionExpired      = "DP00800001029"
)

// Encryption Keys family, DP02010…
const (
	CodeEncryptionFailed  = "DP02010001000"
	CodeEncryptionSuccess = "DP02010001001"
)

// KYC family, DP02200…
const (
	CodeKYCFailed    = "DP02200000000"
	CodeKYCSuccess   = "DP02200000001"
	CodeKYCNotFound  = "DP02200000002"
	CodeAccountFail  = "DP02100000000"
	CodeAccountOK    = "DP02100000001"
	CodeAccountNoUsr = "DP02100000002"
)

// Gateway layer, ROUTER…
const (
	CodeRouterNoWallet        = "ROUTER001"
	CodeRouterMissingHeader   = "ROUTER003"
	CodeRouterNoCountryRoute  = "ROUTER005"
	CodeRouterBadCountry      = "ROUTER006"
	CodeRouterCountryDenied   = "ROUTER007"
	CodeRouterBadCurrency     = "ROUTER112"
	CodeRouterPinValidation   = "ROUTER114"
	CodeRouterIncorrectPin    = "ROUTER115"
	CodeRouterBadEncryptedPin = "ROUTER116"
	CodeRouterTimeout         = "ROUTER117"
	CodeRouterMissingCurrency = "ROUTER119"
)

// Application layer, the legacy result_code vocabulary.
const (
	CodeESBSomethingWrong  = "ESB000001"
	CodeESBInitiateFailed  = "ESB000004"
	CodeESBValidation      = "ESB000008"
	CodeESBSuccess         = "ESB000010"
	CodeESBFailed          = "ESB000011"
	CodeESBStatusFetch     = "ESB000014"
	CodeESBBadMSISDNLength = "ESB000033"
	CodeESBBadCountry      = "ESB000034"
	CodeESBBadCurrency     = "ESB000035"
	CodeESBBadMSISDN       = "ESB000036"
	CodeESBNoVendor        = "ESB000039"
	CodeESBDuplicateExtID  = "ESB000041"
	CodeESBNoTransaction   = "ESB000045"
	CodeESBAmbiguous       = "0000900"
)

// pendingCodes are the outcomes that mean the transaction may still succeed.
// They are not failures and must never be returned as errors: the documented
// response to every one of them is to enquire, and a caller handed an error
// has no value to enquire about.
var pendingCodes = map[string]struct{}{
	CodeCollectionAmbiguous: {},
	CodeCollectionInProcess: {},
	CodeCollectionTimedOut:  {},
	CodeESBSomethingWrong:   {},
	CodeESBInitiateFailed:   {},
	CodeESBValidation:       {},
	CodeESBStatusFetch:      {},
	CodeESBAmbiguous:        {},
	CodeRouterTimeout:       {},
}

// successCodes are the per-family spellings of success. The envelope's own
// success flag is authoritative; these cover the products that set a success
// code while leaving the flag unset.
var successCodes = map[string]struct{}{
	CodeCollectionSuccess: {},
	CodeEncryptionSuccess: {},
	CodeKYCSuccess:        {},
	CodeAccountOK:         {},
	CodeESBSuccess:        {},
}

// IsPending reports whether a response code means "enquire, do not retry".
func IsPending(code string) bool {
	_, ok := pendingCodes[code]
	return ok
}

// parseError turns a non-2xx into a structured error.
func parseError(errb oops.OopsErrorBuilder, status int, raw []byte) error {
	var body struct {
		Status Status `json:"status"`
	}
	// A best-effort decode: a gateway that answers with an HTML error page
	// still has to produce an error, and the raw body becomes the message.
	_ = json.Unmarshal(raw, &body)

	if body.Status.zero() {
		body.Status.Message = strings.TrimSpace(string(raw))
	}
	return statusError(errb, status, body.Status)
}

// checkEnvelope converts an unsuccessful 2xx envelope into an error. A pending
// code is not unsuccessful, whatever the success flag says.
func checkEnvelope(errb oops.OopsErrorBuilder, httpStatus int, s Status) error {
	if s.zero() {
		return nil
	}
	code := lo.CoalesceOrEmpty(s.ResponseCode, s.ResultCode)
	if IsPending(code) {
		return nil
	}
	if s.Success {
		return nil
	}
	if _, ok := successCodes[code]; ok {
		return nil
	}
	return statusError(errb, httpStatus, s)
}

func statusError(errb oops.OopsErrorBuilder, httpStatus int, s Status) error {
	wrapped := &AirtelError{StatusCode: httpStatus, Status: s}

	errb = errb.With(pkgErrors.AttrStatusCode, httpStatus)
	if s.ResponseCode != "" {
		errb = errb.With("airtel_response_code", s.ResponseCode)
	}
	if s.ResultCode != "" {
		errb = errb.With("airtel_result_code", s.ResultCode)
	}

	code, hint := classify(httpStatus, s)
	errb = errb.Code(code)
	if hint != "" {
		errb = errb.Hint(hint)
	}
	return errb.Wrapf(wrapped, "Airtel rejected the request")
}

// classify maps an Airtel response code to the shared vocabulary.
//
// Three layers can answer before the product does — the Kong gateway with
// ROUTER codes, the application layer with the legacy ESB codes, and HTTP
// itself — so the product code is checked first and the coarser layers behind
// it.
func classify(httpStatus int, s Status) (code, hint string) {
	responseCode := lo.CoalesceOrEmpty(s.ResponseCode, s.ResultCode)

	switch responseCode {
	case CodeCollectionForbidden:
		return pkgErrors.CodePermissionDenied,
			"x-signature did not match the payload. This is our signing, not the payer: check the AES key, the IV and that the RSA key from EncryptionKeys has not expired."
	case CodeRouterBadEncryptedPin:
		return pkgErrors.CodePermissionDenied,
			"The encrypted PIN was rejected by the gateway. This is our RSA encryption, not a wrong PIN — ROUTER115 is the wrong PIN."
	case CodeRouterIncorrectPin, CodeCollectionIncorrectPin:
		return pkgErrors.CodePermissionDenied, "The payer entered the wrong PIN."
	case CodeCollectionNoPinEntered:
		return pkgErrors.CodePermissionDenied, "The payer never entered a PIN; the prompt was not completed."
	case CodeCollectionNoBalance:
		return pkgErrors.CodeInsufficientLiquidity, "The payer's wallet cannot cover the amount."
	case CodeCollectionLimit:
		return pkgErrors.CodeInvalidAmount, "The payer's wallet transaction limit was exceeded."
	case CodeCollectionInvalidAmt:
		return pkgErrors.CodeInvalidAmount, "The amount is below Airtel's minimum transferable amount."
	case CodeCollectionPayeeBarred:
		return pkgErrors.CodeMerchantNotPermitted, "The payee is churned, barred, or not registered on Airtel Money."
	case CodeCollectionRefused, CodeCollectionDoNotHonor:
		return pkgErrors.CodePermissionDenied, "Airtel refused the transaction."
	case CodeCollectionNotFound, CodeESBNoTransaction:
		return pkgErrors.CodeNotFound, "No transaction exists with that id."
	case CodeCollectionExpired:
		return pkgErrors.CodeQuoteExpired, "The transaction expired before the payer acted."
	case CodeESBDuplicateExtID:
		return pkgErrors.CodeDuplicateRequest,
			"This transaction id already reached Airtel. Enquire for the original's status; do not send it again with a fresh id."
	case CodeRouterNoWallet, CodeRouterNoCountryRoute, CodeRouterCountryDenied:
		return pkgErrors.CodeSubscriptionUnavailable,
			"The application is not provisioned for this country or wallet. A commercial gap, not a transient failure."
	case CodeRouterMissingHeader:
		return pkgErrors.CodeBuildFailed,
			"A mandatory header or body field was missing. Check the country and currency header casing for this endpoint."
	case CodeRouterBadCountry, CodeRouterBadCurrency, CodeRouterMissingCurrency,
		CodeESBBadCountry, CodeESBBadCurrency:
		return pkgErrors.CodeBuildFailed, ""
	case CodeESBBadMSISDN, CodeESBBadMSISDNLength:
		return pkgErrors.CodeMissingPhoneNumber,
			"Airtel rejected the MSISDN. Collection takes the national form with no country code."
	case CodeESBNoVendor:
		return pkgErrors.CodeSubscriptionUnavailable, ""
	case CodeEncryptionFailed:
		return pkgErrors.CodeHTTPError, "Airtel could not return an encryption key; signing cannot proceed."
	case CodeKYCNotFound, CodeAccountNoUsr:
		return pkgErrors.CodeNotFound, "No Airtel Money user for that MSISDN."
	}

	switch httpStatus {
	case http.StatusUnauthorized:
		return pkgErrors.CodeUnauthorized, "Mint a fresh access token; Airtel tokens live 180 seconds."
	case http.StatusForbidden:
		return pkgErrors.CodePermissionDenied, ""
	case http.StatusNotFound:
		return pkgErrors.CodeNotFound, ""
	case http.StatusTooManyRequests:
		return pkgErrors.CodeRateLimited, "Back off rather than retrying immediately."
	case http.StatusRequestTimeout, http.StatusBadGateway, http.StatusGatewayTimeout:
		return pkgErrors.CodeHTTPError,
			"For payments and refunds this is an enquiry, not a retry: the transaction may have completed."
	}
	return pkgErrors.CodeHTTPError, ""
}

// isTokenRejected reports whether err is Airtel refusing our access token.
// Airtel documents no dedicated code for it, so the HTTP status is all there
// is to go on.
func isTokenRejected(err error) bool {
	var a *AirtelError
	if !errors.As(err, &a) {
		return false
	}
	if a.StatusCode == http.StatusUnauthorized {
		return true
	}
	return strings.Contains(strings.ToLower(a.Status.Message), "invalid access token")
}
