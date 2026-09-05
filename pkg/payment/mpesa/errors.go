package mpesa

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

// APIError is Daraja's synchronous error body.
type APIError struct {
	RequestID    string `json:"requestId"`
	ErrorCode    string `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`
}

// DarajaError is a synchronous rejection. It is wrapped by the oops error the
// package returns, so callers can reach it with errors.As to inspect the exact
// Daraja code rather than the coarse pkgErrors code.
type DarajaError struct {
	StatusCode int
	RequestID  string
	Code       string
	Message    string
}

func (e *DarajaError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("daraja returned %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("daraja %s: %s", e.Code, e.Message)
}

// ResultError is an asynchronous result that reported failure. The result code
// namespace is per-API — 2001 is an invalid initiator on Reversal and a wrong
// customer PIN on M-Pesa Express — so each endpoint maps its own codes to
// outcomes and this type carries the raw values for the ones that do not.
type ResultError struct {
	ResultCode               int64
	ResultDesc               string
	ConversationID           string
	OriginatorConversationID string
	TransactionID            string
}

func (e *ResultError) Error() string {
	return fmt.Sprintf("daraja result %d: %s", e.ResultCode, e.ResultDesc)
}

// Synchronous Daraja error codes. Daraja spells the same condition differently
// per API, which is why these are grouped rather than switched on individually.
const (
	errCodeInvalidPayload      = "400.002.05"
	errCodeBadRequest          = "400.002.02"
	errCodeBadRequestAlt       = "400.003.02"
	errCodeInvalidAuthType     = "400.008.01"
	errCodeInvalidGrantType    = "400.008.02"
	errCodeMethodNotAllowed    = "405.001"
	errCodeSpikeArrest         = "500.003.02"
	errCodeQuotaViolation      = "500.003.03"
	errCodeInternalServer      = "500.003.1001"
	errCodeSTKInternal         = "500.001.1001"
	errCodeDuplicateOriginator = "500.002.1001"
)

// tokenRejectedCodes are the four spellings Daraja uses for a rejected access
// token. They differ per API, and all four must trigger the single re-mint in
// call — matching only one leaves three APIs unable to recover from a token
// invalidated by another replica.
var tokenRejectedCodes = map[string]struct{}{
	"404.001.03": {}, // M-Pesa Express, Reversal
	"400.003.01": {}, // Transaction Status, C2B
	"401.002.01": {}, // Account Balance
	"401.001":    {}, // Pull Transaction
}

// notFoundCodes are the three spellings of a missing resource.
var notFoundCodes = map[string]struct{}{
	"404.001.01": {},
	"404.002.01": {},
	"404.003.01": {},
}

// invalidAuthHeaderCode is returned when a POST-only endpoint is called with
// misplaced headers.
const invalidAuthHeaderCode = "404.001.04"

// parseError turns a non-2xx into a structured error.
func parseError(errb oops.OopsErrorBuilder, status int, raw []byte) error {
	var apiErr APIError
	_ = json.Unmarshal(raw, &apiErr)

	message := lo.CoalesceOrEmpty(apiErr.ErrorMessage, strings.TrimSpace(string(raw)))
	wrapped := &DarajaError{
		StatusCode: status,
		RequestID:  apiErr.RequestID,
		Code:       apiErr.ErrorCode,
		Message:    message,
	}

	errb = errb.With(pkgErrors.AttrStatusCode, status)
	if apiErr.ErrorCode != "" {
		errb = errb.With("daraja_code", apiErr.ErrorCode)
	}
	if apiErr.RequestID != "" {
		errb = errb.With("daraja_request_id", apiErr.RequestID)
	}

	code, hint := classify(status, apiErr.ErrorCode, message)
	errb = errb.Code(code)
	if hint != "" {
		errb = errb.Hint(hint)
	}
	return errb.Wrapf(wrapped, "Daraja rejected the request")
}

// classify maps a Daraja error code to the shared vocabulary.
//
// Message inspection is unavoidable for the 500.001.1001 family: Safaricom
// overloads that one code across "Merchant does not exist", "Wrong credentials"
// and "Unable to lock subscriber", which are three different actions.
func classify(status int, darajaCode, message string) (code, hint string) {
	lower := strings.ToLower(message)

	if isTokenRejectedCode(darajaCode) || strings.Contains(lower, "invalid access token") {
		return pkgErrors.CodeUnauthorized, "Regenerate the access token; Daraja invalidates the previous one on every mint."
	}
	if _, ok := notFoundCodes[darajaCode]; ok {
		return pkgErrors.CodeNotFound, "Check the endpoint path for the environment."
	}

	switch darajaCode {
	case invalidAuthHeaderCode, errCodeInvalidAuthType, errCodeInvalidGrantType:
		return pkgErrors.CodeUnauthorized, ""
	case errCodeBadRequest, errCodeBadRequestAlt, errCodeInvalidPayload, errCodeMethodNotAllowed:
		return pkgErrors.CodeBuildFailed, ""
	case errCodeSpikeArrest, errCodeQuotaViolation:
		// TODO: CodeRateLimited — a retryable throttle is worth alerting on
		// separately from a generic non-2xx.
		return pkgErrors.CodeHTTPError, "Daraja is throttling; back off rather than retrying immediately."
	case errCodeDuplicateOriginator:
		return pkgErrors.CodeDuplicateRequest, "This exact request already reached M-Pesa. Resolve the original with TransactionStatus; do not send it again."
	case errCodeSTKInternal:
		switch {
		case strings.Contains(lower, "lock subscriber"):
			// TODO: CodeSubscriberLocked — a transaction is already in flight
			// for this MSISDN, which is a wait, not a failure.
			return pkgErrors.CodeDuplicateRequest, "A prompt is already in flight for this subscriber. Wait at least a minute."
		case strings.Contains(lower, "merchant does not exist"):
			return pkgErrors.CodeUnauthorized, "The shortcode does not match the one the app went live with."
		case strings.Contains(lower, "credential"), strings.Contains(lower, "password"):
			return pkgErrors.CodeUnauthorized, ""
		}
		return pkgErrors.CodeHTTPError, ""
	case errCodeInternalServer:
		if strings.Contains(lower, "already registered") || strings.Contains(lower, "duplicate notification") {
			return pkgErrors.CodeDuplicateRequest, "URLs are already registered for this shortcode; registration is one-time and must be undone by Safaricom."
		}
		return pkgErrors.CodeHTTPError, ""
	}

	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return pkgErrors.CodeUnauthorized, ""
	case http.StatusNotFound:
		return pkgErrors.CodeNotFound, ""
	}
	return pkgErrors.CodeHTTPError, ""
}

func isTokenRejectedCode(darajaCode string) bool {
	_, ok := tokenRejectedCodes[darajaCode]
	return ok
}

// isTokenRejected reports whether err is Daraja refusing our access token, in
// any of its four spellings.
func isTokenRejected(err error) bool {
	var d *DarajaError
	if !errors.As(err, &d) {
		return false
	}
	return isTokenRejectedCode(d.Code) ||
		strings.Contains(strings.ToLower(d.Message), "invalid access token")
}

// resultError builds the error for a failed asynchronous result. code is the
// caller's mapping for the endpoint, since the result code namespace is
// per-API.
func resultError(errb oops.OopsErrorBuilder, code string, r *ResultError) error {
	return errb.
		Code(code).
		With("daraja_result_code", r.ResultCode).
		With("daraja_conversation_id", r.ConversationID).
		Wrapf(r, "Daraja reported a failed result")
}
