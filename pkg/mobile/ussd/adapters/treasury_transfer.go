package adapters

import (
	"context"
	"fmt"

	"github.com/Shamba-Records-Limited/microvault/internal/core/services/stellar/types"
)

// StellarSendUSDC is the subset of the Stellar service needed for treasury USDC transfers.
type StellarSendUSDC interface {
	// SendUSDC sends USDC from the treasury wallet to the destination specified in the request.
	SendUSDC(ctx context.Context, req types.SendUSDCRequest) (*types.SendUSDCResponse, error)
}

// StellarTreasuryTransfer adapts the Stellar service's SendUSDC method
// to the TreasuryTransfer interface used by the OffRamp adapter.
type StellarTreasuryTransfer struct {
	stellar StellarSendUSDC
}

// NewStellarTreasuryTransfer creates a new TreasuryTransfer backed by the Stellar service.
func NewStellarTreasuryTransfer(stellar StellarSendUSDC) *StellarTreasuryTransfer {
	return &StellarTreasuryTransfer{stellar: stellar}
}

var _ TreasuryTransfer = (*StellarTreasuryTransfer)(nil)

// SendUSDC sends USDC from the treasury wallet to a destination address with a memo.
func (t *StellarTreasuryTransfer) SendUSDC(ctx context.Context, destination string, memo string, amount int64) (string, error) {
	resp, err := t.stellar.SendUSDC(ctx, types.SendUSDCRequest{
		Destination: destination,
		Memo:        memo,
		Amount:      amount,
	})
	if err != nil {
		return "", fmt.Errorf("treasury USDC transfer failed: %w", err)
	}
	return resp.TxHash, nil
}
