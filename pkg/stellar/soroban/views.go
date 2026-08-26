package soroban

import (
	"context"

	"github.com/samber/oops"
	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stellar/go-stellar-sdk/xdr"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/types"
)

// viewErr starts an error builder scoped to one read-only contract call.
func viewErr(fnName string) oops.OopsErrorBuilder {
	return oops.
		In(errDomain).
		Tags("soroban", "view").
		With(pkgErrors.AttrContractFunction, fnName)
}

// callView simulates a read-only contract call and decodes its return value.
// Every view goes through here, so the build, simulate, reject, empty-result
// and decode failures are classified once rather than eleven times.
func (s *service) callView(ctx context.Context, fnName string, args []xdr.ScVal) (xdr.ScVal, error) {
	errb := viewErr(fnName)

	op, err := s.buildInvokeContractOp(fnName, args)
	if err != nil {
		return xdr.ScVal{}, errb.Code(pkgErrors.CodeBuildFailed).
			Wrapf(err, "could not build contract invocation")
	}

	adminKP := keypair.MustParseFull(s.adminPrivateKey)

	simResp, err := s.simulateContractCall(ctx, adminKP.Address(), op)
	if err != nil {
		return xdr.ScVal{}, errb.Code(pkgErrors.CodeSimulationFailed).
			Wrapf(err, "contract simulation could not be performed")
	}

	if simResp.Error != "" {
		return xdr.ScVal{}, errb.
			Code(pkgErrors.CodeSimulationRejected).
			With("simulation_error", simResp.Error).
			Wrapf(types.ErrSimulationFailed, "contract simulation rejected the call")
	}

	// ReturnValueXDR is a pointer and was dereferenced unchecked at every call
	// site before this consolidation.
	if len(simResp.Results) == 0 || simResp.Results[0].ReturnValueXDR == nil {
		return xdr.ScVal{}, errb.Code(pkgErrors.CodeIncompleteResponse).
			Wrapf(types.ErrNoSimulationResult, "simulation returned no value")
	}

	var result xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(*simResp.Results[0].ReturnValueXDR, &result); err != nil {
		return xdr.ScVal{}, errb.Code(pkgErrors.CodeDecodeFailed).
			Wrapf(err, "could not decode the simulation result")
	}

	return result, nil
}

// userViewArgs converts a user address into the single argument the per-user
// views take.
func userViewArgs(fnName, userAddress string) ([]xdr.ScVal, error) {
	userAddr, err := addressToScVal(userAddress)
	if err != nil {
		return nil, viewErr(fnName).Code(pkgErrors.CodeInvalidAddress).
			With(pkgErrors.AttrAddress, userAddress).
			Wrapf(err, "invalid user address")
	}
	return []xdr.ScVal{userAddr}, nil
}

// ============================================================================
// View Functions (Read-Only)
// ============================================================================

// GetTreasuryAddress returns the treasury address from the vault contract
func (s *service) GetTreasuryAddress(ctx context.Context) (string, error) {
	result, err := s.callView(ctx, "treasury", nil)
	if err != nil {
		return "", err
	}
	return scValToAddress(result)
}

// GetTotalBorrowed returns the total amount borrowed from the vault
func (s *service) GetTotalBorrowed(ctx context.Context) (int64, error) {
	result, err := s.callView(ctx, "total_borrowed", nil)
	if err != nil {
		return 0, err
	}
	return scValToI128(result)
}

// GetAvailableLiquidity returns the available liquidity in the vault
func (s *service) GetAvailableLiquidity(ctx context.Context) (int64, error) {
	result, err := s.callView(ctx, "available_liquidity", nil)
	if err != nil {
		return 0, err
	}
	return scValToI128(result)
}

// GetTotalManagedAssets returns the total assets under management
func (s *service) GetTotalManagedAssets(ctx context.Context) (int64, error) {
	result, err := s.callView(ctx, "total_managed_assets", nil)
	if err != nil {
		return 0, err
	}
	return scValToI128(result)
}

// GetUtilizationRate returns the current utilization rate
func (s *service) GetUtilizationRate(ctx context.Context) (int64, error) {
	result, err := s.callView(ctx, "utilization_rate", nil)
	if err != nil {
		return 0, err
	}
	return scValToI128(result)
}

// GetBorrowAPR returns the current borrow APR
func (s *service) GetBorrowAPR(ctx context.Context) (int64, error) {
	result, err := s.callView(ctx, "borrow_apr", nil)
	if err != nil {
		return 0, err
	}
	return scValToI128(result)
}

// GetBorrowIndex returns the cumulative borrow index from the vault (WAD scale)
func (s *service) GetBorrowIndex(ctx context.Context) (int64, error) {
	result, err := s.callView(ctx, "get_borrow_index", nil)
	if err != nil {
		return 0, err
	}
	return scValToI128(result)
}

// IsUserLocked checks if a user's vault shares are locked
func (s *service) IsUserLocked(ctx context.Context, userAddress string) (bool, error) {
	const fnName = "is_locked"

	args, err := userViewArgs(fnName, userAddress)
	if err != nil {
		return false, err
	}

	result, err := s.callView(ctx, fnName, args)
	if err != nil {
		return false, err
	}
	return scValToBool(result)
}

// GetLockPeriod returns the lock period in seconds
func (s *service) GetLockPeriod(ctx context.Context) (uint64, error) {
	result, err := s.callView(ctx, "get_lock_period", nil)
	if err != nil {
		return 0, err
	}
	return scValToU64(result)
}

// GetRemainingLockTime returns the remaining lock time for a user
func (s *service) GetRemainingLockTime(ctx context.Context, userAddress string) (uint64, error) {
	const fnName = "remaining_lock_time"

	args, err := userViewArgs(fnName, userAddress)
	if err != nil {
		return 0, err
	}

	result, err := s.callView(ctx, fnName, args)
	if err != nil {
		return 0, err
	}
	return scValToU64(result)
}

// IsPaused checks if the vault is currently paused
func (s *service) IsPaused(ctx context.Context) (bool, error) {
	result, err := s.callView(ctx, "paused", nil)
	if err != nil {
		return false, err
	}
	return scValToBool(result)
}
