// Package adapters provides core implementation for USSD integration for stellar treasury transfers.
package adapters

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/offramp"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/types"
)

// StellarSendUSDC is the subset of the Stellar service needed for treasury USDC transfers.
type StellarSendUSDC interface {
	// SendUSDC sends USDC from the treasury wallet to the destination specified in the request.
	SendUSDC(ctx context.Context, req types.SendUSDCRequest) (*types.SendUSDCResponse, error)
	// CheckUSDCTrustline checks if an address has a trustline for our USDC asset.
	CheckUSDCTrustline(ctx context.Context, address string) (bool, error)
}

// StellarTreasuryTransfer adapts the Stellar service's SendUSDC method
// to the TreasuryTransfer interface used by the OffRamp adapter.
type StellarTreasuryTransfer struct {
	stellar StellarSendUSDC
	logger  *slog.Logger
}

// NewStellarTreasuryTransfer creates a new TreasuryTransfer backed by the Stellar service.
func NewStellarTreasuryTransfer(stellar StellarSendUSDC, logger *slog.Logger) *StellarTreasuryTransfer {
	if logger == nil {
		logger = slog.Default()
	}
	return &StellarTreasuryTransfer{
		stellar: stellar,
		logger:  logger.With("component", "treasury_transfer"),
	}
}

var _ offramp.TreasuryTransfer = (*StellarTreasuryTransfer)(nil)

// SendUSDC sends USDC from the treasury wallet to a destination address with a memo.
func (t *StellarTreasuryTransfer) SendUSDC(ctx context.Context, destination string, memo string, amount int64) (string, error) {
	t.logger.Info("initiating treasury USDC transfer",
		"destination", destination,
		"memo", memo,
		"amount_stroops", amount,
		"amount_usdc", float64(amount)/1e7,
	)

	resp, err := t.stellar.SendUSDC(ctx, types.SendUSDCRequest{
		Destination: destination,
		Memo:        memo,
		Amount:      amount,
	})
	if err != nil {
		t.logger.Error("treasury USDC transfer failed",
			"destination", destination,
			"memo", memo,
			"amount_stroops", amount,
			"error", err,
		)
		return "", fmt.Errorf("treasury USDC transfer failed: %w", err)
	}

	t.logger.Info("treasury USDC transfer succeeded",
		"tx_hash", resp.TxHash,
		"ledger", resp.Ledger,
		"status", resp.Status,
		"destination", destination,
		"memo", memo,
		"amount_stroops", amount,
		"amount_usdc", float64(amount)/1e7,
	)
	return resp.TxHash, nil
}

// CheckUSDCTrustline checks if an external address has a trustline for our USDC asset.
func (t *StellarTreasuryTransfer) CheckUSDCTrustline(ctx context.Context, address string) (bool, error) {
	return t.stellar.CheckUSDCTrustline(ctx, address)
}
