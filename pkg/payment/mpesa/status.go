package mpesa

import (
	"context"
	"net/http"
	"strconv"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

const pathTransactionStatus = "/mpesa/transactionstatus/v1/query"

// TransactionStatusRequest asks Daraja what became of a transaction.
//
// This is the resolution path for anything a queue timeout left unknown. It is
// the only way to turn "we do not know" into a fact without guessing, and it is
// the reason a timeout must never be retried.
type TransactionStatusRequest struct {
	// TransactionID is the M-Pesa receipt. Supply this or
	// OriginalConversationID.
	TransactionID string

	// OriginalConversationID identifies a request whose receipt we never saw.
	OriginalConversationID string

	// PartyA defaults to the configured collection shortcode.
	PartyA         uint
	IdentifierType PartyIdentifierType

	Remarks  string
	Occasion string

	URLs AsyncURLs
}

// TransactionStatus asks Daraja to resolve a transaction. The answer arrives at
// the result URL, not here.
func (c *Client) TransactionStatus(ctx context.Context, req TransactionStatusRequest) (*AsyncAck, error) {
	errb := mpesaErr("transaction_status").With("transaction_id", req.TransactionID)

	if req.TransactionID == "" && req.OriginalConversationID == "" {
		return nil, errb.
			Code(pkgErrors.CodeMissingDependency).
			Errorf("either a transaction ID or an original conversation ID is required")
	}
	if req.PartyA == 0 {
		req.PartyA = c.collectionShortcode
	}
	if req.IdentifierType == "" {
		req.IdentifierType = IdentifierShortcode
	}
	if req.Remarks == "" {
		req.Remarks = "Status query"
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
		"CommandID":              "TransactionStatusQuery",
		"TransactionID":          req.TransactionID,
		"OriginalConversationID": req.OriginalConversationID,
		"PartyA":                 strconv.FormatUint(uint64(req.PartyA), 10),
		"IdentifierType":         string(req.IdentifierType),
		"ResultURL":              req.URLs.ResultURL,
		"QueueTimeOutURL":        req.URLs.QueueTimeOutURL,
		"Remarks":                req.Remarks,
		"Occasion":               req.Occasion,
	}
	return call[AsyncAck](ctx, c, errb, http.MethodPost, pathTransactionStatus, body)
}
