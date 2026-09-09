package mpesa

import (
	"context"
	"net/http"
	"strconv"
)

const pathAccountBalance = "/mpesa/accountbalance/v1/query"

// AccountBalanceRequest asks for a shortcode's account balances.
//
// It is also the cheapest end-to-end check of the Initiator and
// SecurityCredential path: if this works, the credentials Reversal and
// Transaction Status depend on are correct. Running it before trusting a
// reversal is worth the call.
type AccountBalanceRequest struct {
	// PartyA defaults to the configured collection shortcode.
	PartyA         uint
	IdentifierType PartyIdentifierType
	Remarks        string
	URLs           AsyncURLs
}

// AccountBalance asks Daraja for the shortcode balances. The figures arrive at
// the result URL as a pipe-delimited string; see ParseBalances.
func (c *Client) AccountBalance(ctx context.Context, req AccountBalanceRequest) (*AsyncAck, error) {
	errb := mpesaErr("account_balance")

	if req.PartyA == 0 {
		req.PartyA = c.collectionShortcode
	}
	if req.IdentifierType == "" {
		req.IdentifierType = IdentifierShortcode
	}
	if req.Remarks == "" {
		req.Remarks = "Balance query"
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
		"Initiator":          c.initiatorName,
		"SecurityCredential": credential,
		"CommandID":          "AccountBalance",
		"PartyA":             strconv.FormatUint(uint64(req.PartyA), 10),
		"IdentifierType":     string(req.IdentifierType),
		"Remarks":            req.Remarks,
		"ResultURL":          req.URLs.ResultURL,
		"QueueTimeOutURL":    req.URLs.QueueTimeOutURL,
	}
	return call[AsyncAck](ctx, c, errb.With("shortcode", req.PartyA), http.MethodPost, pathAccountBalance, body)
}
