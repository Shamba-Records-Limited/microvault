package mpesa

import (
	"context"
	"net/http"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Query Organization Info — B2B Hakikisha, despite the name an outbound call
// we make, not a server we stand up (that reframing is §6 of the plan;
// contrast with C2B Hakikisha, which is the reverse and is what
// daraja_hakikisha_controller.go implements). Package capability only —
// nothing calls it yet, since nothing here sends B2B disbursements to
// resolve first.

const pathOrgInfo = "/sfcverify/v1/query/info"

// The success code. The sample response reads "4000"; the error-code table on
// the same documentation page says 0 is success, which is boilerplate copied
// from elsewhere on the site — 4000 is what the sample actually returns, and
// what OrgInfoResponse.Success checks. Fail closed on everything else,
// including 0: a verifier that answers "verified" for an unrecognised code is
// worse than no verifier.
const orgInfoSuccess = "4000"

// OrgIdentifierType distinguishes a paybill from a till on this endpoint
// specifically. It is its own type rather than PartyIdentifierType because
// the two disagree on what "2" means: here it is a till, where
// IdentifierTillOwner ("2") means something else entirely on Transaction
// Status and Account Balance. Passing one where the other belongs would be a
// call that succeeds against the wrong semantics.
type OrgIdentifierType string

// The two identifier types this endpoint accepts.
const (
	OrgIdentifierTill    OrgIdentifierType = "2"
	OrgIdentifierPaybill OrgIdentifierType = "4"
)

// OrgInfoRequest names the shortcode to resolve.
type OrgInfoRequest struct {
	IdentifierType OrgIdentifierType
	// Identifier is the till or paybill number, as a string on the wire.
	Identifier string
}

// OrgInfoResponse answers with the organisation's trading name and tariff.
type OrgInfoResponse struct {
	ConversationID        string `json:"ConversationID"`
	ResponseCode          string `json:"ResponseCode"`
	ResponseMessage       string `json:"ResponseMessage"`
	DetailedMessage       string `json:"DetailedMessage"`
	OrganizationShortCode string `json:"OrganizationShortCode"`
	OrganizationName      string `json:"OrganizationName"`
	// ChargeProfileID determines who bears the B2B transaction's cost. The
	// published profile-ID mapping is partial and the documentation's own
	// sample returns an ID absent from it — record it verbatim and map it
	// when known; never fail on an unrecognised value.
	ChargeProfileID string `json:"ChargeProfileID"`
}

// Success reports whether Safaricom resolved the identifier. See
// orgInfoSuccess for why this checks "4000" and not "0".
func (r OrgInfoResponse) Success() bool { return r.ResponseCode == orgInfoSuccess }

// QueryOrgInfo resolves a shortcode to its trading name — the guard the plan
// recommends in front of every B2B disbursement, since paying the wrong
// paybill is not reversible the way an over-collection is. Synchronous,
// bearer-only: no Initiator, no SecurityCredential, no callback, and
// (unlike every other Initiator-bearing call in this package) nothing here
// can lock the API operator's password.
func (c *Client) QueryOrgInfo(ctx context.Context, req OrgInfoRequest) (*OrgInfoResponse, error) {
	errb := mpesaErr("query_org_info").With("identifier", req.Identifier)

	if req.IdentifierType != OrgIdentifierTill && req.IdentifierType != OrgIdentifierPaybill {
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("identifier type must be till or paybill")
	}
	if req.Identifier == "" {
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("identifier is required")
	}

	body := map[string]string{
		"IdentifierType": string(req.IdentifierType),
		"Identifier":     req.Identifier,
	}
	return call[OrgInfoResponse](ctx, c, errb, http.MethodPost, pathOrgInfo, body)
}
