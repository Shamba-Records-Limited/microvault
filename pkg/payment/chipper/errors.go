package chipper

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/samber/lo"
	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

var errInvalidBaseURL = errors.New("chipper: base url must be https")

const maxErrorBodyBytes = 512

// Stable error codes from the error envelope.
const (
	CodeValidation            = "validation_error"
	CodeBadRequest            = "bad_request"
	CodeInsufficientFunds     = "insufficient_funds"
	CodePayoutLimitExceeded   = "payout_limit_exceeded"
	CodeUnauthorized          = "unauthorized"
	CodeForbidden             = "forbidden"
	CodeOrganizationSuspended = "organization_suspended"
	CodeNotFound              = "not_found"
	CodeDuplicate             = "duplicate"
	CodeConflict              = "conflict"
	CodeInvalidRecipient      = "invalid_recipient"
	CodeRateLimited           = "rate_limited"
	CodeNoProviderAvailable   = "no_provider_available"
	CodeRateUnavailable       = "rate_unavailable"
	CodeInternal              = "internal_error"
)

// APIError is a non-2xx response. It is wrapped by the returned oops error;
// reach it with AsAPIError.
type APIError struct {
	StatusCode int             `json:"-"`
	Code       string          `json:"error"`
	Message    string          `json:"message"`
	Details    json.RawMessage `json:"details,omitempty"`
	RequestID  string          `json:"requestId"`
	RetryAfter time.Duration   `json:"-"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("chipper %d %s: %s", e.StatusCode, lo.CoalesceOrEmpty(e.Code, "unknown"), lo.CoalesceOrEmpty(e.Message, "no message"))
}

// Retryable reports whether the same request may succeed later.
func (e *APIError) Retryable() bool {
	return e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= http.StatusInternalServerError
}

// AsAPIError extracts the APIError from err.
func AsAPIError(err error) (*APIError, bool) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}

type errBuilder = oops.OopsErrorBuilder

func chipperErr(op string) oops.OopsErrorBuilder {
	return oops.In(pkgErrors.DomainOffRamp).
		Tags(ProviderName).
		With(pkgErrors.AttrProvider, ProviderName).
		With(pkgErrors.AttrOperation, op)
}

func parseError(errb oops.OopsErrorBuilder, resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))

	apiErr := &APIError{StatusCode: resp.StatusCode}
	if err := json.Unmarshal(raw, apiErr); err != nil || apiErr.Code == "" {
		apiErr.Message = lo.CoalesceOrEmpty(apiErr.Message, truncate(string(raw), maxErrorBodyBytes))
	}
	apiErr.RequestID = lo.CoalesceOrEmpty(apiErr.RequestID, resp.Header.Get("x-request-id"))
	apiErr.RetryAfter = retryAfter(apiErr.Details, resp.Header.Get("Retry-After"))

	errb = errb.
		With(pkgErrors.AttrStatusCode, resp.StatusCode).
		With("api_code", apiErr.Code).
		With("request_id", apiErr.RequestID)

	return errb.Code(pkgCode(apiErr)).Wrapf(apiErr, "chipper rejected the request")
}

func pkgCode(e *APIError) string {
	switch e.Code {
	case CodeUnauthorized:
		return pkgErrors.CodeUnauthorized
	case CodeForbidden, CodeOrganizationSuspended:
		return pkgErrors.CodePermissionDenied
	case CodeNotFound:
		return pkgErrors.CodeNotFound
	case CodeDuplicate:
		return pkgErrors.CodeDuplicateRequest
	case CodeRateLimited:
		return pkgErrors.CodeRateLimited
	case CodeInsufficientFunds:
		return pkgErrors.CodeInsufficientLiquidity
	case CodeRateUnavailable:
		return pkgErrors.CodeRateUnavailable
	case CodeNoProviderAvailable:
		return pkgErrors.CodeCorridorUnavailable
	case CodePayoutLimitExceeded:
		return pkgErrors.CodeInvalidAmount
	case CodeInvalidRecipient:
		return pkgErrors.CodeInvalidAddress
	}
	switch e.StatusCode {
	case http.StatusUnauthorized:
		return pkgErrors.CodeUnauthorized
	case http.StatusForbidden:
		return pkgErrors.CodePermissionDenied
	case http.StatusNotFound:
		return pkgErrors.CodeNotFound
	case http.StatusTooManyRequests:
		return pkgErrors.CodeRateLimited
	}
	return pkgErrors.CodeHTTPError
}

func retryAfter(details json.RawMessage, header string) time.Duration {
	var d struct {
		RetryAfter json.Number `json:"retryAfter"`
	}
	if len(details) > 0 && json.Unmarshal(details, &d) == nil && d.RetryAfter != "" {
		if secs, err := d.RetryAfter.Float64(); err == nil && secs > 0 {
			return time.Duration(secs * float64(time.Second))
		}
	}
	if secs, err := strconv.Atoi(header); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}
