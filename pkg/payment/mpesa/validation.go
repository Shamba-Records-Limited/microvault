package mpesa

import (
	"context"
	"net/http"
	"strconv"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/phone"
)

const pathValidateID = "/v1/KYC-validation/validateID"

// IDType selects the identity document a number is checked against.
type IDType string

// The document types Safaricom accepts.
const (
	IDTypeNational IDType = "01"
	IDTypeMilitary IDType = "02"
	IDTypePassport IDType = "05"
)

// Mobile Number Validation response codes.
const (
	validationMatched   = "4000"
	validationUnmatched = "4001"
)

// ValidationPolicy is how a caller intends to act on a verdict. The package
// records it and acts on nothing; enforcement belongs to whoever owns the
// registration and payment flows.
type ValidationPolicy string

// The three settings.
//
// Advisory is the one to run first whatever the eventual answer: it records
// mismatches without blocking anyone, which measures whether an upstream
// platform is already validating rather than assuming it.
const (
	// ValidationDisabled does not call at all. Correct when an upstream
	// platform already validates.
	ValidationDisabled ValidationPolicy = "disabled"

	// ValidationAdvisory calls, records, and never blocks.
	ValidationAdvisory ValidationPolicy = "advisory"

	// ValidationEnforcing blocks on a mismatch.
	ValidationEnforcing ValidationPolicy = "enforcing"
)

// NumberValidation is the verdict.
type NumberValidation struct {
	ResponseRefID string
	ResponseCode  string
	Message       string

	// Matched is true only on the documented success code. Anything
	// unrecognised is false.
	Matched bool
}

type validateIDRequest struct {
	RequestRefID string `json:"requestRefID"`
	ShortCode    string `json:"shortCode"`
	MSISDN       string `json:"msisdn"`
	IDType       string `json:"idType"`
	IDNumber     string `json:"idNumber"`
}

type validateIDResponse struct {
	ResponseRefID   string `json:"responseRefID"`
	ResponseCode    string `json:"responseCode"`
	ResponseMessage string `json:"responseMessage"`

	// Status is the string "true" or "false", not a boolean. Decoding it into
	// a bool fails, and a decoder that swallowed the failure would turn "no
	// match" into a zero-valued "match".
	Status string `json:"status"`
}

// ValidateMobileNumber checks that an MSISDN is registered under an ID number.
//
// It fails closed. Only the documented match code counts as a match; every
// other response, documented or not, is a non-match. A verifier that answers
// "verified" to a code it does not understand is worse than no verifier,
// because it is trusted.
//
// Call this out of band. It is a paid synchronous round trip to a third party
// and the C2B validation window is eight seconds, so blocking a payment on it
// means one slow response stalls every concurrent payment.
func (c *Client) ValidateMobileNumber(ctx context.Context, msisdn string, idType IDType, idNumber string, shortcode uint) (*NumberValidation, error) {
	errb := mpesaErr("validate_mobile_number").With("msisdn", phone.Redact(msisdn))

	if idNumber == "" {
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("ID number is required")
	}
	if idType == "" {
		idType = IDTypeNational
	}
	if shortcode == 0 {
		shortcode = c.collectionShortcode
	}

	normalized, err := NormalizeMSISDN(msisdn)
	if err != nil {
		return nil, err
	}

	body := validateIDRequest{
		RequestRefID: strconv.FormatInt(c.now().UnixNano(), 10),
		ShortCode:    strconv.FormatUint(uint64(shortcode), 10),
		MSISDN:       normalized,
		IDType:       string(idType),
		IDNumber:     idNumber,
	}

	response, err := call[validateIDResponse](ctx, c, errb, http.MethodPost, pathValidateID, body)
	if err != nil {
		return nil, err
	}
	return &NumberValidation{
		ResponseRefID: response.ResponseRefID,
		ResponseCode:  response.ResponseCode,
		Message:       response.ResponseMessage,
		Matched:       response.ResponseCode == validationMatched && response.Status == "true",
	}, nil
}
