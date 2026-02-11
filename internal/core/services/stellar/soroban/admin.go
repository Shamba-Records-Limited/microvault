package soroban

import (
	"context"
	"fmt"
	"log"

	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// ============================================================================
// Admin Operations (Owner Only)
// These require the contract owner's signature
// ============================================================================

// PauseVault pauses all vault operations
func (s *service) PauseVault(ctx context.Context) error {
	adminKP := keypair.MustParseFull(s.adminPrivateKey)

	callerAddr, _ := addressToScVal(adminKP.Address())
	args := []xdr.ScVal{callerAddr}

	op, err := s.buildInvokeContractOp("pause", args)
	if err != nil {
		return err
	}

	simResp, err := s.simulateContractCall(ctx, adminKP.Address(), op)
	if err != nil {
		return fmt.Errorf("simulation failed: %w", err)
	}

	if simResp.Error != "" {
		log.Printf("PauseVault simulation error: %s", simResp.Error)
		return fmt.Errorf("simulation error: %s", simResp.Error)
	}

	txResp, err := s.submitContractTransaction(ctx, adminKP, op, simResp)
	if err != nil {
		return fmt.Errorf("failed to pause vault: %w", err)
	}

	log.Printf("PauseVault: vault paused successfully (tx: %s)", txResp.TransactionHash)
	return nil
}

// UnpauseVault resumes all vault operations
func (s *service) UnpauseVault(ctx context.Context) error {
	adminKP := keypair.MustParseFull(s.adminPrivateKey)

	callerAddr, _ := addressToScVal(adminKP.Address())
	args := []xdr.ScVal{callerAddr}

	op, err := s.buildInvokeContractOp("unpause", args)
	if err != nil {
		return err
	}

	simResp, err := s.simulateContractCall(ctx, adminKP.Address(), op)
	if err != nil {
		return fmt.Errorf("simulation failed: %w", err)
	}

	if simResp.Error != "" {
		log.Printf("UnpauseVault simulation error: %s", simResp.Error)
		return fmt.Errorf("simulation error: %s", simResp.Error)
	}

	txResp, err := s.submitContractTransaction(ctx, adminKP, op, simResp)
	if err != nil {
		return fmt.Errorf("failed to unpause vault: %w", err)
	}

	log.Printf("UnpauseVault: vault unpaused successfully (tx: %s)", txResp.TransactionHash)
	return nil
}

// SetMaxDeposit updates the maximum deposit limit
func (s *service) SetMaxDeposit(ctx context.Context, limit int64) error {
	adminKP := keypair.MustParseFull(s.adminPrivateKey)

	limitVal := i128ToScVal(limit)
	args := []xdr.ScVal{limitVal}

	op, err := s.buildInvokeContractOp("set_max_deposit", args)
	if err != nil {
		return err
	}

	simResp, err := s.simulateContractCall(ctx, adminKP.Address(), op)
	if err != nil {
		return fmt.Errorf("simulation failed: %w", err)
	}

	if simResp.Error != "" {
		log.Printf("SetMaxDeposit simulation error: %s", simResp.Error)
		return fmt.Errorf("simulation error: %s", simResp.Error)
	}

	txResp, err := s.submitContractTransaction(ctx, adminKP, op, simResp)
	if err != nil {
		return fmt.Errorf("failed to set max deposit: %w", err)
	}

	log.Printf("SetMaxDeposit: limit set to %d (tx: %s)", limit, txResp.TransactionHash)
	return nil
}

// SetMaxWithdraw updates the maximum withdrawal limit
func (s *service) SetMaxWithdraw(ctx context.Context, limit int64) error {
	adminKP := keypair.MustParseFull(s.adminPrivateKey)

	limitVal := i128ToScVal(limit)
	args := []xdr.ScVal{limitVal}

	op, err := s.buildInvokeContractOp("set_max_withdraw", args)
	if err != nil {
		return err
	}

	simResp, err := s.simulateContractCall(ctx, adminKP.Address(), op)
	if err != nil {
		return fmt.Errorf("simulation failed: %w", err)
	}

	if simResp.Error != "" {
		log.Printf("SetMaxWithdraw simulation error: %s", simResp.Error)
		return fmt.Errorf("simulation error: %s", simResp.Error)
	}

	txResp, err := s.submitContractTransaction(ctx, adminKP, op, simResp)
	if err != nil {
		return fmt.Errorf("failed to set max withdraw: %w", err)
	}

	log.Printf("SetMaxWithdraw: limit set to %d (tx: %s)", limit, txResp.TransactionHash)
	return nil
}

// SetLockPeriod updates the lock period for vault shares
func (s *service) SetLockPeriod(ctx context.Context, periodSeconds uint64) error {
	adminKP := keypair.MustParseFull(s.adminPrivateKey)

	periodVal := u64ToScVal(periodSeconds)
	args := []xdr.ScVal{periodVal}

	op, err := s.buildInvokeContractOp("set_lock_period", args)
	if err != nil {
		return err
	}

	simResp, err := s.simulateContractCall(ctx, adminKP.Address(), op)
	if err != nil {
		return fmt.Errorf("simulation failed: %w", err)
	}

	if simResp.Error != "" {
		log.Printf("SetLockPeriod simulation error: %s", simResp.Error)
		return fmt.Errorf("simulation error: %s", simResp.Error)
	}

	txResp, err := s.submitContractTransaction(ctx, adminKP, op, simResp)
	if err != nil {
		return fmt.Errorf("failed to set lock period: %w", err)
	}

	log.Printf("SetLockPeriod: period set to %d seconds (tx: %s)", periodSeconds, txResp.TransactionHash)
	return nil
}
