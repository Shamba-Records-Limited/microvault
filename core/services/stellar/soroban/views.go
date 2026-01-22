package soroban

import (
	"context"
	"fmt"

	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/Shamba-Records-Limited/Microvault/internal/core/services/stellar/types"
)

// ============================================================================
// View Functions (Read-Only)
// ============================================================================

// GetTreasuryAddress returns the treasury address from the vault contract
func (s *service) GetTreasuryAddress(ctx context.Context) (string, error) {
	op, err := s.buildInvokeContractOp("treasury", nil)
	if err != nil {
		return "", err
	}

	adminKP := keypair.MustParseFull(s.adminPrivateKey)
	simResp, err := s.simulateContractCall(ctx, adminKP.Address(), op)
	if err != nil {
		return "", err
	}

	if simResp.Error != "" {
		return "", fmt.Errorf("simulation error: %s", simResp.Error)
	}

	if len(simResp.Results) == 0 {
		return "", types.ErrNoSimulationResult
	}

	var resultXDR xdr.ScVal
	err = xdr.SafeUnmarshalBase64(*simResp.Results[0].ReturnValueXDR, &resultXDR)
	if err != nil {
		return "", fmt.Errorf("failed to decode result: %w", err)
	}

	return scValToAddress(resultXDR)
}

// GetTotalBorrowed returns the total amount borrowed from the vault
func (s *service) GetTotalBorrowed(ctx context.Context) (int64, error) {
	op, err := s.buildInvokeContractOp("total_borrowed", nil)
	if err != nil {
		return 0, err
	}

	adminKP := keypair.MustParseFull(s.adminPrivateKey)
	simResp, err := s.simulateContractCall(ctx, adminKP.Address(), op)
	if err != nil {
		return 0, err
	}

	if simResp.Error != "" {
		return 0, fmt.Errorf("simulation error: %s", simResp.Error)
	}

	if len(simResp.Results) == 0 {
		return 0, types.ErrNoSimulationResult
	}

	var resultXDR xdr.ScVal
	err = xdr.SafeUnmarshalBase64(*simResp.Results[0].ReturnValueXDR, &resultXDR)
	if err != nil {
		return 0, fmt.Errorf("failed to decode result: %w", err)
	}

	return scValToI128(resultXDR)
}

// GetAvailableLiquidity returns the available liquidity in the vault
func (s *service) GetAvailableLiquidity(ctx context.Context) (int64, error) {
	op, err := s.buildInvokeContractOp("available_liquidity", nil)
	if err != nil {
		return 0, err
	}

	adminKP := keypair.MustParseFull(s.adminPrivateKey)
	simResp, err := s.simulateContractCall(ctx, adminKP.Address(), op)
	if err != nil {
		return 0, err
	}

	if simResp.Error != "" {
		return 0, fmt.Errorf("simulation error: %s", simResp.Error)
	}

	if len(simResp.Results) == 0 {
		return 0, types.ErrNoSimulationResult
	}

	var resultXDR xdr.ScVal
	err = xdr.SafeUnmarshalBase64(*simResp.Results[0].ReturnValueXDR, &resultXDR)
	if err != nil {
		return 0, fmt.Errorf("failed to decode result: %w", err)
	}

	return scValToI128(resultXDR)
}

// GetTotalManagedAssets returns the total assets under management
func (s *service) GetTotalManagedAssets(ctx context.Context) (int64, error) {
	op, err := s.buildInvokeContractOp("total_managed_assets", nil)
	if err != nil {
		return 0, err
	}

	adminKP := keypair.MustParseFull(s.adminPrivateKey)
	simResp, err := s.simulateContractCall(ctx, adminKP.Address(), op)
	if err != nil {
		return 0, err
	}

	if simResp.Error != "" {
		return 0, fmt.Errorf("simulation error: %s", simResp.Error)
	}

	if len(simResp.Results) == 0 {
		return 0, types.ErrNoSimulationResult
	}

	var resultXDR xdr.ScVal
	err = xdr.SafeUnmarshalBase64(*simResp.Results[0].ReturnValueXDR, &resultXDR)
	if err != nil {
		return 0, fmt.Errorf("failed to decode result: %w", err)
	}

	return scValToI128(resultXDR)
}

// GetUtilizationRate returns the current utilization rate
func (s *service) GetUtilizationRate(ctx context.Context) (int64, error) {
	op, err := s.buildInvokeContractOp("utilization_rate", nil)
	if err != nil {
		return 0, err
	}

	adminKP := keypair.MustParseFull(s.adminPrivateKey)
	simResp, err := s.simulateContractCall(ctx, adminKP.Address(), op)
	if err != nil {
		return 0, err
	}

	if simResp.Error != "" {
		return 0, fmt.Errorf("simulation error: %s", simResp.Error)
	}

	if len(simResp.Results) == 0 {
		return 0, types.ErrNoSimulationResult
	}

	var resultXDR xdr.ScVal
	err = xdr.SafeUnmarshalBase64(*simResp.Results[0].ReturnValueXDR, &resultXDR)
	if err != nil {
		return 0, fmt.Errorf("failed to decode result: %w", err)
	}

	return scValToI128(resultXDR)
}

// GetBorrowAPR returns the current borrow APR
func (s *service) GetBorrowAPR(ctx context.Context) (int64, error) {
	op, err := s.buildInvokeContractOp("borrow_apr", nil)
	if err != nil {
		return 0, err
	}

	adminKP := keypair.MustParseFull(s.adminPrivateKey)
	simResp, err := s.simulateContractCall(ctx, adminKP.Address(), op)
	if err != nil {
		return 0, err
	}

	if simResp.Error != "" {
		return 0, fmt.Errorf("simulation error: %s", simResp.Error)
	}

	if len(simResp.Results) == 0 {
		return 0, types.ErrNoSimulationResult
	}

	var resultXDR xdr.ScVal
	err = xdr.SafeUnmarshalBase64(*simResp.Results[0].ReturnValueXDR, &resultXDR)
	if err != nil {
		return 0, fmt.Errorf("failed to decode result: %w", err)
	}

	return scValToI128(resultXDR)
}

// IsUserLocked checks if a user's vault shares are locked
func (s *service) IsUserLocked(ctx context.Context, userAddress string) (bool, error) {
	userAddr, err := addressToScVal(userAddress)
	if err != nil {
		return false, fmt.Errorf("invalid user address: %w", err)
	}

	op, err := s.buildInvokeContractOp("is_locked", []xdr.ScVal{userAddr})
	if err != nil {
		return false, err
	}

	adminKP := keypair.MustParseFull(s.adminPrivateKey)
	simResp, err := s.simulateContractCall(ctx, adminKP.Address(), op)
	if err != nil {
		return false, err
	}

	if simResp.Error != "" {
		return false, fmt.Errorf("simulation error: %s", simResp.Error)
	}

	if len(simResp.Results) == 0 {
		return false, types.ErrNoSimulationResult
	}

	var resultXDR xdr.ScVal
	err = xdr.SafeUnmarshalBase64(*simResp.Results[0].ReturnValueXDR, &resultXDR)
	if err != nil {
		return false, fmt.Errorf("failed to decode result: %w", err)
	}

	return scValToBool(resultXDR)
}

// GetLockPeriod returns the lock period in seconds
func (s *service) GetLockPeriod(ctx context.Context) (uint64, error) {
	op, err := s.buildInvokeContractOp("get_lock_period", nil)
	if err != nil {
		return 0, err
	}

	adminKP := keypair.MustParseFull(s.adminPrivateKey)
	simResp, err := s.simulateContractCall(ctx, adminKP.Address(), op)
	if err != nil {
		return 0, err
	}

	if simResp.Error != "" {
		return 0, fmt.Errorf("simulation error: %s", simResp.Error)
	}

	if len(simResp.Results) == 0 {
		return 0, types.ErrNoSimulationResult
	}

	var resultXDR xdr.ScVal
	err = xdr.SafeUnmarshalBase64(*simResp.Results[0].ReturnValueXDR, &resultXDR)
	if err != nil {
		return 0, fmt.Errorf("failed to decode result: %w", err)
	}

	return scValToU64(resultXDR)
}

// GetRemainingLockTime returns the remaining lock time for a user
func (s *service) GetRemainingLockTime(ctx context.Context, userAddress string) (uint64, error) {
	userAddr, err := addressToScVal(userAddress)
	if err != nil {
		return 0, fmt.Errorf("invalid user address: %w", err)
	}

	op, err := s.buildInvokeContractOp("remaining_lock_time", []xdr.ScVal{userAddr})
	if err != nil {
		return 0, err
	}

	adminKP := keypair.MustParseFull(s.adminPrivateKey)
	simResp, err := s.simulateContractCall(ctx, adminKP.Address(), op)
	if err != nil {
		return 0, err
	}

	if simResp.Error != "" {
		return 0, fmt.Errorf("simulation error: %s", simResp.Error)
	}

	if len(simResp.Results) == 0 {
		return 0, types.ErrNoSimulationResult
	}

	var resultXDR xdr.ScVal
	err = xdr.SafeUnmarshalBase64(*simResp.Results[0].ReturnValueXDR, &resultXDR)
	if err != nil {
		return 0, fmt.Errorf("failed to decode result: %w", err)
	}

	return scValToU64(resultXDR)
}

// IsPaused checks if the vault is currently paused
func (s *service) IsPaused(ctx context.Context) (bool, error) {
	op, err := s.buildInvokeContractOp("paused", nil)
	if err != nil {
		return false, err
	}

	adminKP := keypair.MustParseFull(s.adminPrivateKey)
	simResp, err := s.simulateContractCall(ctx, adminKP.Address(), op)
	if err != nil {
		return false, err
	}

	if simResp.Error != "" {
		return false, fmt.Errorf("simulation error: %s", simResp.Error)
	}

	if len(simResp.Results) == 0 {
		return false, types.ErrNoSimulationResult
	}

	var resultXDR xdr.ScVal
	err = xdr.SafeUnmarshalBase64(*simResp.Results[0].ReturnValueXDR, &resultXDR)
	if err != nil {
		return false, fmt.Errorf("failed to decode result: %w", err)
	}

	return scValToBool(resultXDR)
}
