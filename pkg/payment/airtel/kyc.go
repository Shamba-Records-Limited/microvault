package airtel

import (
	"context"
	"net/http"
	"net/url"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

const pathUserEnquiry = "/standard/v1/users/"

var epUserEnquiry = endpoint{method: http.MethodGet, path: pathUserEnquiry, headers: headerUpper}

// UserEnquiryResponse is Airtel's record of a subscriber.
type UserEnquiryResponse struct {
	Data struct {
		FirstName    string `json:"first_name"`
		LastName     string `json:"last_name"`
		MSISDN       string `json:"msisdn"`
		Grade        string `json:"grade"`
		IsBarred     bool   `json:"is_barred"`
		IsPINSet     bool   `json:"is_pin_set"`
		Registration struct {
			Status string `json:"status"`
		} `json:"registration"`
	} `json:"data"`
	Status Status `json:"status"`
}

func (r *UserEnquiryResponse) envelope() Status { return r.Status }

// CanTransact reports whether a payment prompt to this subscriber can
// succeed at all.
//
// A barred wallet cannot transact and a subscriber with no PIN set cannot
// authorise one, so both turn into a failed payment after the prompt has been
// pushed and the borrower has been told to expect it. Checking first turns a
// downstream DP00800001010 into an answer before any money moves.
func (r UserEnquiryResponse) CanTransact() bool {
	return !r.Data.IsBarred && r.Data.IsPINSet
}

// UserEnquiry looks up a subscriber's wallet state.
func (c *Client) UserEnquiry(ctx context.Context, msisdn string) (*UserEnquiryResponse, error) {
	errb := airtelErr("user_enquiry")

	normalized, err := NormalizeMSISDN(msisdn)
	if err != nil {
		return nil, err
	}
	if normalized == "" {
		return nil, errb.Code(pkgErrors.CodeMissingPhoneNumber).Errorf("no phone number was supplied")
	}

	path := pathUserEnquiry + url.PathEscape(normalized.String())
	return call[UserEnquiryResponse](ctx, c, errb, epUserEnquiry, path, nil)
}
