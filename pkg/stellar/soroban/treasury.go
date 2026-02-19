package soroban

import (
	"context"
	"fmt"
	"log"

	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/types"
)

// ============================================================================
// Treasury Operations (Mutative)
// ============================================================================

// BorrowFromVault allows treasury to borrow funds and send to a recipient
func (s *service) BorrowFromVault(ctx context.Context, req types.BorrowRequest) (*types.BorrowResponse, error) {
	if req.Amount <= 0 {
		return nil, types.ErrInvalidTransactionAmount
	}

	treasuryKP := keypair.MustParseFull(s.treasuryPrivateKey)

	// Build arguments
	treasuryAddr, _ := addressToScVal(treasuryKP.Address())
	recipientAddr, err := addressToScVal(req.RecipientAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid recipient address: %w", err)
	}
	amountVal := i128ToScVal(req.Amount)

	args := []xdr.ScVal{treasuryAddr, recipientAddr, amountVal}

	op, err := s.buildInvokeContractOp("borrow", args)
	if err != nil {
		return nil, err
	}

	// Simulate first to get auth and resource estimates
	simResp, err := s.simulateContractCall(ctx, treasuryKP.Address(), op)
	if err != nil {
		return nil, fmt.Errorf("simulation failed: %w", err)
	}

	if simResp.Error != "" {
		log.Printf("BorrowFromVault simulation error: %s", simResp.Error)
		return nil, fmt.Errorf("simulation error: %s", simResp.Error)
	}

	// Submit transaction
	txResp, err := s.submitContractTransaction(ctx, treasuryKP, op, simResp)
	if err != nil {
		return nil, fmt.Errorf("failed to submit borrow transaction: %w", err)
	}

	// Extract the Borrowed event's recipient field from the transaction result metadata.
	// This serves as the on-chain memo linking the borrow to the child account.
	var eventRecipient string
	if txResp.ResultMetaXDR != "" {
		eventRecipient, _ = extractBorrowedRecipient(txResp.ResultMetaXDR, s.contractID)
	}

	// Fetch current borrow index after the borrow has been processed.
	borrowIndex, err := s.GetBorrowIndex(ctx)
	if err != nil {
		log.Printf("BorrowFromVault: failed to fetch borrow index: %v", err)
		// Non-fatal: borrow succeeded, index is supplementary data.
	}

	log.Printf("BorrowFromVault: %d to %s (tx: %s, event_recipient: %s, borrow_index: %d)",
		req.Amount, req.RecipientAddress, txResp.TransactionHash, eventRecipient, borrowIndex)

	return &types.BorrowResponse{
		TxHash:           txResp.TransactionHash,
		AmountBorrowed:   req.Amount,
		RecipientAddress: req.RecipientAddress,
		EventRecipient:   eventRecipient,
		BorrowIndex:      borrowIndex,
	}, nil
}

// RepayToVault allows treasury to repay borrowed funds
func (s *service) RepayToVault(ctx context.Context, req types.RepayRequest) (*types.RepayResponse, error) {
	if req.Amount <= 0 {
		return nil, types.ErrInvalidTransactionAmount
	}

	treasuryKP := keypair.MustParseFull(s.treasuryPrivateKey)

	// Build arguments
	treasuryAddr, _ := addressToScVal(treasuryKP.Address())
	amountVal := i128ToScVal(req.Amount)

	args := []xdr.ScVal{treasuryAddr, amountVal}

	op, err := s.buildInvokeContractOp("repay", args)
	if err != nil {
		return nil, err
	}

	// Simulate
	simResp, err := s.simulateContractCall(ctx, treasuryKP.Address(), op)
	if err != nil {
		return nil, fmt.Errorf("simulation failed: %w", err)
	}

	if simResp.Error != "" {
		log.Printf("RepayToVault simulation error: %s", simResp.Error)
		return nil, fmt.Errorf("simulation error: %s", simResp.Error)
	}

	// Submit
	txResp, err := s.submitContractTransaction(ctx, treasuryKP, op, simResp)
	if err != nil {
		return nil, fmt.Errorf("failed to submit repay transaction: %w", err)
	}

	log.Printf("RepayToVault: %d repaid (tx: %s)", req.Amount, txResp.TransactionHash)

	return &types.RepayResponse{
		TxHash:       txResp.TransactionHash,
		AmountRepaid: req.Amount,
	}, nil
}

// AccrueInterest triggers interest accrual on the vault
func (s *service) AccrueInterest(ctx context.Context) error {
	treasuryKP := keypair.MustParseFull(s.treasuryPrivateKey)

	op, err := s.buildInvokeContractOp("accrue", nil)
	if err != nil {
		return err
	}

	// Simulate
	simResp, err := s.simulateContractCall(ctx, treasuryKP.Address(), op)
	if err != nil {
		return fmt.Errorf("simulation failed: %w", err)
	}

	if simResp.Error != "" {
		log.Printf("AccrueInterest simulation error: %s", simResp.Error)
		return fmt.Errorf("simulation error: %s", simResp.Error)
	}

	// Submit
	txResp, err := s.submitContractTransaction(ctx, treasuryKP, op, simResp)
	if err != nil {
		return fmt.Errorf("failed to submit accrue transaction: %w", err)
	}

	log.Printf("AccrueInterest: interest accrued (tx: %s)", txResp.TransactionHash)
	return nil
}
