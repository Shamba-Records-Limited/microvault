package merkle

import (
	"context"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/compliance"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

var (
	_ compliance.Screener            = (*Client)(nil)
	_ compliance.TransactionScreener = (*Client)(nil)
)

// ScreenAddress screens a Stellar address as a deposit source.
func (c *Client) ScreenAddress(ctx context.Context, req compliance.ScreenRequest) (*compliance.Screening, error) {
	const op = "screen_address"
	if req.Address == "" {
		return nil, merkleErr(op).Code(pkgErrors.CodeMissingAccount).Errorf("address is empty")
	}
	var resp screenResponse
	raw, err := c.post(ctx, op, "/addresses/", screenRequest{
		Identifier: req.Address,
		Blockchain: stellarBlockchain,
		CustomerID: req.CustomerReference,
		Type:       typeDeposit,
		ShowAlerts: true,
	}, &resp)
	if err != nil {
		return nil, err
	}
	return c.screening(resp, raw, req.Address, "", true), nil
}

// ScreenTransaction screens a Stellar transaction into the vault from the
// customer's address.
func (c *Client) ScreenTransaction(ctx context.Context, req compliance.TxScreenRequest) (*compliance.Screening, error) {
	const op = "screen_transaction"
	if req.Hash == "" {
		return nil, merkleErr(op).Code(pkgErrors.CodeBuildFailed).Errorf("transaction hash is empty")
	}
	var resp screenResponse
	raw, err := c.post(ctx, op, "/transactions/", screenRequest{
		Identifier: req.Hash,
		Blockchain: stellarBlockchain,
		CustomerID: req.CustomerReference,
		Address:    req.Address,
		Type:       typeDeposit,
		ShowAlerts: true,
	}, &resp)
	if err != nil {
		return nil, err
	}
	return c.screening(resp, raw, req.Address, req.Hash, false), nil
}

func (c *Client) screening(resp screenResponse, raw []byte, address, hash string, isAddress bool) *compliance.Screening {
	var score *float64
	if resp.RiskLevel != nil {
		v := float64(*resp.RiskLevel)
		score = &v
	}
	return &compliance.Screening{
		Provider:    Provider,
		ProviderRef: resp.RiskLevelVerbose,
		Address:     address,
		TxHash:      hash,
		Verdict:     deriveVerdict(resp, c.thresholds, isAddress),
		RiskScore:   score,
		Sanctioned:  sanctioned(resp),
		Rules:       rules(resp),
		ScreenedAt:  time.Now(),
		Raw:         raw,
	}
}
