package mpesa

import (
	"context"
	"net/http"
	"strconv"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

const pathReversal = "/mpesa/reversal/v1/request"

// ReversalRequest returns a received payment to the payer.
//
// This is the most dangerous call in the package: it moves real money and
// cannot be undone. The package has no database and therefore cannot check the
// preconditions that make a reversal safe. Before calling this the caller must
// have established, in one place:
//
//  1. The transaction was observed from Safaricom, not merely asserted by a
//     callback. Daraja signs nothing, so an unconfirmed callback is not
//     evidence that a payment exists.
//  2. It resolves to no open loan, or to one outside our domain.
//  3. It has not credited a loan. If it has, the correct action is a refund
//     decision, not a reversal.
//  4. No reversal is already in flight or complete for it, enforced by a unique
//     index rather than a read.
//  5. The amount matches the observed amount exactly.
//  6. We hold the initiator role on the receiving shortcode.
//
// Reversals should not be automatic. The failure modes of an automated reversal
// loop are unbounded and irreversible; the cost of a human in the loop is hours
// of delay on an already exceptional case.
type ReversalRequest struct {
	// TransactionID is the M-Pesa receipt being reversed.
	TransactionID string

	// AmountKES must equal the amount originally received.
	AmountKES int64

	// ReceiverParty defaults to the configured collection shortcode: the
	// shortcode that received the money and will give it back.
	ReceiverParty uint

	Remarks  string
	Occasion string

	URLs AsyncURLs
}

// Reverse returns a payment to its payer.
//
// CommandID, RecieverIdentifierType and SecurityCredential are set here rather
// than taken from the caller, because each has exactly one correct value and
// none of them is guessable. Note Safaricom's misspelling of "Receiver" in the
// wire field, which must be reproduced exactly.
func (c *Client) Reverse(ctx context.Context, req ReversalRequest) (*AsyncAck, error) {
	errb := mpesaErr("reversal").With("transaction_id", req.TransactionID)

	if req.TransactionID == "" {
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("transaction ID is required")
	}
	if req.AmountKES <= 0 {
		return nil, errb.Code(pkgErrors.CodeInvalidAmount).Errorf("amount must be positive")
	}
	if req.ReceiverParty == 0 {
		req.ReceiverParty = c.collectionShortcode
	}
	if req.ReceiverParty == 0 {
		return nil, errb.
			Code(pkgErrors.CodeMissingDependency).
			With(pkgErrors.AttrDependency, "receiver shortcode").
			Errorf("no receiving shortcode was supplied or configured")
	}
	if req.Remarks == "" {
		req.Remarks = "Payment reversal"
	}
	if err := validateRemarks(errb, req.Remarks); err != nil {
		return nil, err
	}
	if err := req.URLs.validate(errb); err != nil {
		return nil, err
	}

	credential, err := c.SecurityCredential()
	if err != nil {
		return nil, err
	}

	body := map[string]string{
		"Initiator":              c.initiatorName,
		"SecurityCredential":     credential,
		"CommandID":              "TransactionReversal",
		"TransactionID":          req.TransactionID,
		"Amount":                 strconv.FormatInt(req.AmountKES, 10),
		"ReceiverParty":          strconv.FormatUint(uint64(req.ReceiverParty), 10),
		"RecieverIdentifierType": string(ReversalIdentifierShortcode),
		"ResultURL":              req.URLs.ResultURL,
		"QueueTimeOutURL":        req.URLs.QueueTimeOutURL,
		"Remarks":                req.Remarks,
		"Occasion":               req.Occasion,
	}
	return call[AsyncAck](ctx, c, errb, http.MethodPost, pathReversal, body)
}
