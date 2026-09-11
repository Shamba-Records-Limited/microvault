package mpesa

import (
	"encoding/json"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// C2B Hakikisha inverts the direction of every other API here: Safaricom is the
// client and we are the server. It fires on any payment to the shortcode, from
// any channel, and asks us to name the account the payer typed so their handset
// can show it before they confirm.
//
// What this file provides is everything the server needs except the server: the
// wire types, the resolver contract, and the encoding. Standing up the HTTP
// route and the OAuth issuer Safaricom authenticates against is wiring.

// HakikishaRequest is what Safaricom asks us.
type HakikishaRequest struct {
	// AccountNumber is the reference the payer typed on their handset, so it
	// arrives exactly as they typed it.
	AccountNumber string `json:"accountNumber" example:"MV7K3QA9"`

	// ShortCode is the M-Pesa paybill/till the payer is sending to.
	ShortCode string `json:"shortCode" example:"174379"`

	// Timestamp arrives as either a string or a number.
	Timestamp FlexibleInt64 `json:"timestamp" example:"20260911120000"`

	// TransactionID is Safaricom's identifier for this validation request.
	TransactionID string `json:"transactionId" example:"OEI2AK4Q16"`
}

// HakikishaResponse is what we answer.
//
// AccountName is shown to whichever M-Pesa customer supplied the account
// number, which makes this endpoint a name-disclosure oracle for anyone who can
// guess a reference. It must not carry a borrower's name. Something that
// identifies the obligation without identifying the person — "Microvault Loan
// MV7K3QA9" — tells the payer what they need and nobody else anything.
type HakikishaResponse struct {
	// AccountName is shown to the payer on their handset; must identify the obligation, never the borrower. Empty when not found.
	AccountName string `json:"accountName" example:"Microvault Loan MV7K3QA9"`
	// AccountNumber echoes back the reference that was resolved.
	AccountNumber string `json:"accountNumber" example:"MV7K3QA9"`
	// ResponseCode is HakikishaFound ("0") or HakikishaNotFound ("1").
	ResponseCode string `json:"responseCode" example:"0"`
	// ResponseDesc is a short human-readable status, e.g. "Success" or "Account not found".
	ResponseDesc string `json:"responseDesc" example:"Success"`
}

// Hakikisha response codes.
const (
	HakikishaFound    = "0"
	HakikishaNotFound = "1"
)

// AccountResolver answers Safaricom's question.
//
// Implementations must be fast: this sits in front of a customer holding a
// handset, and it must not consult anything slower than one indexed lookup.
// They must also never place a person's name in AccountName; see
// HakikishaResponse.
type AccountResolver interface {
	ResolveAccount(accountNumber string) (accountName string, found bool, err error)
}

// ParseHakikishaRequest decodes an inbound request.
func ParseHakikishaRequest(raw []byte) (*HakikishaRequest, error) {
	var req HakikishaRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, mpesaErr("parse_hakikisha_request").
			Code(pkgErrors.CodeDecodeFailed).
			Wrapf(err, "could not decode the Hakikisha request")
	}
	return &req, nil
}

// AccountFound builds an affirmative answer.
func AccountFound(accountNumber, accountName string) HakikishaResponse {
	return HakikishaResponse{
		AccountName:   accountName,
		AccountNumber: accountNumber,
		ResponseCode:  HakikishaFound,
		ResponseDesc:  "Success",
	}
}

// AccountNotFound builds a negative answer. It carries no account name, so an
// unknown reference discloses nothing at all.
func AccountNotFound(accountNumber string) HakikishaResponse {
	return HakikishaResponse{
		AccountNumber: accountNumber,
		ResponseCode:  HakikishaNotFound,
		ResponseDesc:  "Account not found",
	}
}
