package soroban

import (
	"context"
	"log"

	"github.com/samber/oops"
	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stellar/go-stellar-sdk/xdr"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// adminErr starts an error builder scoped to one owner-signed contract call.
func adminErr(fnName string) oops.OopsErrorBuilder {
	return oops.
		In(errDomain).
		Tags("soroban", "admin").
		With(pkgErrors.AttrContractFunction, fnName)
}

// ============================================================================
// Admin Operations (Owner Only)
// These require the contract owner's signature
// ============================================================================

// PauseVault pauses all vault operations
func (s *service) PauseVault(ctx context.Context) error {
	const fnName = "pause"

	adminKP := keypair.MustParseFull(s.adminPrivateKey)
	callerAddr, _ := addressToScVal(adminKP.Address())

	txResp, err := s.invokeSigned(ctx, adminKP, fnName, []xdr.ScVal{callerAddr}, adminErr(fnName))
	if err != nil {
		return err
	}

	log.Printf("PauseVault: vault paused successfully (tx: %s)", txResp.TransactionHash)
	return nil
}

// UnpauseVault resumes all vault operations
func (s *service) UnpauseVault(ctx context.Context) error {
	const fnName = "unpause"

	adminKP := keypair.MustParseFull(s.adminPrivateKey)
	callerAddr, _ := addressToScVal(adminKP.Address())

	txResp, err := s.invokeSigned(ctx, adminKP, fnName, []xdr.ScVal{callerAddr}, adminErr(fnName))
	if err != nil {
		return err
	}

	log.Printf("UnpauseVault: vault unpaused successfully (tx: %s)", txResp.TransactionHash)
	return nil
}

// SetMaxDeposit updates the maximum deposit limit
func (s *service) SetMaxDeposit(ctx context.Context, limit int64) error {
	const fnName = "set_max_deposit"

	adminKP := keypair.MustParseFull(s.adminPrivateKey)
	errb := adminErr(fnName).With(pkgErrors.AttrAmountStroops, limit)

	txResp, err := s.invokeSigned(ctx, adminKP, fnName, []xdr.ScVal{i128ToScVal(limit)}, errb)
	if err != nil {
		return err
	}

	log.Printf("SetMaxDeposit: limit set to %d (tx: %s)", limit, txResp.TransactionHash)
	return nil
}

// SetMaxWithdraw updates the maximum withdrawal limit
func (s *service) SetMaxWithdraw(ctx context.Context, limit int64) error {
	const fnName = "set_max_withdraw"

	adminKP := keypair.MustParseFull(s.adminPrivateKey)
	errb := adminErr(fnName).With(pkgErrors.AttrAmountStroops, limit)

	txResp, err := s.invokeSigned(ctx, adminKP, fnName, []xdr.ScVal{i128ToScVal(limit)}, errb)
	if err != nil {
		return err
	}

	log.Printf("SetMaxWithdraw: limit set to %d (tx: %s)", limit, txResp.TransactionHash)
	return nil
}

// SetLockPeriod updates the lock period for vault shares
func (s *service) SetLockPeriod(ctx context.Context, periodSeconds uint64) error {
	const fnName = "set_lock_period"

	adminKP := keypair.MustParseFull(s.adminPrivateKey)
	errb := adminErr(fnName).With("lock_period_seconds", periodSeconds)

	txResp, err := s.invokeSigned(ctx, adminKP, fnName, []xdr.ScVal{u64ToScVal(periodSeconds)}, errb)
	if err != nil {
		return err
	}

	log.Printf("SetLockPeriod: period set to %d seconds (tx: %s)", periodSeconds, txResp.TransactionHash)
	return nil
}
