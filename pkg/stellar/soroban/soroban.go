package soroban

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/Shamba-Records-Limited/microvault/pkg/telemetry"

	"github.com/samber/oops"

	"github.com/stellar/go-stellar-sdk/clients/rpcclient"
	"github.com/stellar/go-stellar-sdk/keypair"
	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stellar/go-stellar-sdk/protocols/stellarcore"
	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/txnbuild"
	"github.com/stellar/go-stellar-sdk/xdr"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/rpc"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/types"
)

// Headroom added over simulated Soroban resources before submit, absorbing
// instruction drift between simulate and submit that would otherwise fail with
// scecExceededLimit. Unused resource fee is refunded on-chain.
const (
	sorobanInstructionPadPct = 25
	sorobanResourceFeePadPct = 30
)

// RPCClient defines the interface for Stellar RPC operations
type RPCClient interface {
	LoadAccount(ctx context.Context, address string) (txnbuild.Account, error)
	SimulateTransaction(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error)
	SendTransaction(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error)
	GetTransaction(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error)
}

// Ensure rpcclient.Client implements RPCClient
var _ RPCClient = (*rpcclient.Client)(nil)

// Service defines the interface for Soroban contract operations
type Service interface {
	// View Functions (read-only)
	GetTreasuryAddress(ctx context.Context) (string, error)
	GetTotalBorrowed(ctx context.Context) (int64, error)
	GetAvailableLiquidity(ctx context.Context) (int64, error)
	GetTotalManagedAssets(ctx context.Context) (int64, error)
	GetUtilizationRate(ctx context.Context) (int64, error)
	GetBorrowAPR(ctx context.Context) (int64, error)
	GetBorrowIndex(ctx context.Context) (int64, error)
	IsUserLocked(ctx context.Context, userAddress string) (bool, error)
	IsAllowed(ctx context.Context, userAddress string) (bool, error)
	// ComplianceRole returns the address authorized to call
	// AllowDepositor/DisallowDepositor, or "" if the vault has none set.
	ComplianceRole(ctx context.Context) (string, error)
	// AllowlistEnforced reports whether the vault currently blocks
	// deposits/mints/transfers involving an unallowlisted address.
	AllowlistEnforced(ctx context.Context) (bool, error)
	GetLockPeriod(ctx context.Context) (uint64, error)
	GetRemainingLockTime(ctx context.Context, userAddress string) (uint64, error)
	IsPaused(ctx context.Context) (bool, error)

	// Treasury Operations
	BorrowFromVault(ctx context.Context, req types.BorrowRequest) (*types.BorrowResponse, error)
	RepayToVault(ctx context.Context, req types.RepayRequest) (*types.RepayResponse, error)
	RepayForVault(ctx context.Context, req types.RepayForRequest) (*types.RepayResponse, error)
	BumpYield(ctx context.Context, req types.BumpYieldRequest) (*types.BumpYieldResponse, error)
	AccrueInterest(ctx context.Context) error

	// Admin Operations
	PauseVault(ctx context.Context) error
	UnpauseVault(ctx context.Context) error
	SetMaxDeposit(ctx context.Context, limit int64) error
	SetMaxWithdraw(ctx context.Context, limit int64) error
	SetLockPeriod(ctx context.Context, periodSeconds uint64) error

	// Compliance Operations — see compliance.go. These sign with the
	// compliance role key, not the admin key, per the source design doc
	// §9's "keeps the freeze key away from the configuration key". A
	// Service constructed without WithComplianceRole errors clearly on
	// these rather than falling back to another key.
	AllowDepositor(ctx context.Context, address string) error
	DisallowDepositor(ctx context.Context, address string) error
	// WithComplianceRole sets the signing key for AllowDepositor/
	// DisallowDepositor and returns the same Service, so a caller that
	// doesn't need compliance calls (most of them — five existing
	// construction sites at the time this was added) never has to pass an
	// unused key through the constructor.
	//
	// Not safe to call concurrently with any other use of this Service —
	// it mutates the underlying struct in place with no locking, matching
	// every other field here (adminPrivateKey, treasuryPrivateKey) being
	// set once at construction and never touched again. Call it exactly
	// once, immediately after construction, before starting any goroutine
	// or handler that might use the Service.
	WithComplianceRole(privateKey string) Service
}

type service struct {
	rpcClient                RPCClient
	networkPassphrase        string
	treasuryPrivateKey       string
	adminPrivateKey          string
	complianceRolePrivateKey string
	contractID               string
	logger                   *slog.Logger
}

func (s *service) WithComplianceRole(privateKey string) Service {
	s.complianceRolePrivateKey = privateKey
	return s
}

// NewService creates a new Soroban service
func NewService(
	rpcClient *rpcclient.Client,
	networkPassphrase string,
	treasuryPrivateKey string,
	adminPrivateKey string,
	contractID string,
) Service {
	return &service{
		rpcClient:          rpcClient,
		networkPassphrase:  networkPassphrase,
		treasuryPrivateKey: treasuryPrivateKey,
		adminPrivateKey:    adminPrivateKey,
		contractID:         contractID,
		logger:             slog.Default().With(slog.String("service", "soroban")),
	}
}

// NewServiceWithClient creates a new Soroban service with a custom RPC client
func NewServiceWithClient(
	rpcClient RPCClient,
	networkPassphrase string,
	treasuryPrivateKey string,
	adminPrivateKey string,
	contractID string,
) Service {
	return &service{
		rpcClient:          rpcClient,
		networkPassphrase:  networkPassphrase,
		treasuryPrivateKey: treasuryPrivateKey,
		adminPrivateKey:    adminPrivateKey,
		contractID:         contractID,
		logger:             slog.Default().With(slog.String("service", "soroban")),
	}
}

// ============================================================================
// Core Soroban Methods
// ============================================================================

// coreErr starts an error builder for the build/simulate/submit machinery every
// contract call goes through, whoever signs it.
func coreErr(op string) oops.OopsErrorBuilder {
	return oops.
		In(errDomain).
		Tags("soroban").
		With(pkgErrors.AttrOperation, op)
}

// getContractAddress parses the contract ID and returns an ScAddress
func (s *service) getContractAddress() (xdr.ScAddress, error) {
	contractBytes, err := strkey.Decode(strkey.VersionByteContract, s.contractID)
	if err != nil {
		return xdr.ScAddress{}, coreErr("contract_address").
			Code(pkgErrors.CodeInvalidAddress).
			With("contract_id", s.contractID).
			Wrapf(err, "invalid contract ID")
	}

	var contractID xdr.ContractId
	copy(contractID[:], contractBytes)

	return xdr.NewScAddress(xdr.ScAddressTypeScAddressTypeContract, contractID)
}

// buildInvokeContractOp creates an InvokeHostFunction operation for the contract
func (s *service) buildInvokeContractOp(functionName string, args []xdr.ScVal) (*txnbuild.InvokeHostFunction, error) {
	contractAddr, err := s.getContractAddress()
	if err != nil {
		return nil, err
	}

	return &txnbuild.InvokeHostFunction{
		HostFunction: xdr.HostFunction{
			Type: xdr.HostFunctionTypeHostFunctionTypeInvokeContract,
			InvokeContract: &xdr.InvokeContractArgs{
				ContractAddress: contractAddr,
				FunctionName:    xdr.ScSymbol(functionName),
				Args:            args,
			},
		},
		SourceAccount: "",
	}, nil
}

// simulateContractCall simulates a contract call and returns the response
func (s *service) simulateContractCall(
	ctx context.Context,
	sourceAddress string,
	op *txnbuild.InvokeHostFunction,
) (*protocol.SimulateTransactionResponse, error) {
	errb := coreErr("simulate").With(pkgErrors.AttrAddress, sourceAddress)

	sourceAccount, err := s.rpcClient.LoadAccount(ctx, sourceAddress)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeTransportFailed).
			Wrapf(err, "could not load the source account")
	}

	tx, err := txnbuild.NewTransaction(txnbuild.TransactionParams{
		SourceAccount:        sourceAccount,
		IncrementSequenceNum: true,
		BaseFee:              txnbuild.MinBaseFee,
		Preconditions: txnbuild.Preconditions{
			TimeBounds: txnbuild.NewTimeout(300),
		},
		Operations: []txnbuild.Operation{op},
	})
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeBuildFailed).
			Wrapf(err, "could not build the simulation transaction")
	}

	txXDR, err := tx.Base64()
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeEncodeFailed).
			Wrapf(err, "could not encode the simulation transaction")
	}

	simResp, err := s.rpcClient.SimulateTransaction(ctx, protocol.SimulateTransactionRequest{
		Transaction: txXDR,
	})
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeSimulationFailed).
			Wrapf(err, "the simulation request failed")
	}

	return &simResp, nil
}

// submitContractTransaction submits a signed contract transaction and returns
// the full GetTransactionResponse so callers can inspect result metadata and events.
func (s *service) submitContractTransaction(
	ctx context.Context,
	signerKP *keypair.Full,
	op *txnbuild.InvokeHostFunction,
	simResp *protocol.SimulateTransactionResponse,
	onSigned signedHook,
) (protocol.GetTransactionResponse, error) {
	empty := protocol.GetTransactionResponse{}
	errb := coreErr("submit").With(pkgErrors.AttrAddress, signerKP.Address())

	sourceAccount, err := s.rpcClient.LoadAccount(ctx, signerKP.Address())
	if err != nil {
		return empty, errb.Code(pkgErrors.CodeTransportFailed).
			Wrapf(err, "could not load the source account")
	}

	// Extract auth entries from simulation results
	var authEntries []xdr.SorobanAuthorizationEntry
	if len(simResp.Results) > 0 && simResp.Results[0].AuthXDR != nil {
		for _, authXDR := range *simResp.Results[0].AuthXDR {
			var auth xdr.SorobanAuthorizationEntry
			if err := xdr.SafeUnmarshalBase64(authXDR, &auth); err != nil {
				return empty, errb.Code(pkgErrors.CodeDecodeFailed).
					Wrapf(err, "could not decode a simulation auth entry")
			}
			authEntries = append(authEntries, auth)
		}
	}

	// Set auth from simulation
	op.Auth = authEntries

	// Parse Soroban data and pad the instruction budget before submit:
	// simulated CPU can undershoot actual execution when ledger state drifts
	// between simulate and submit, failing with scecExceededLimit.
	resourceFee := simResp.MinResourceFee
	if simResp.TransactionDataXDR != "" {
		var transactionData xdr.SorobanTransactionData
		if err := xdr.SafeUnmarshalBase64(simResp.TransactionDataXDR, &transactionData); err != nil {
			return empty, errb.Code(pkgErrors.CodeDecodeFailed).
				Wrapf(err, "could not decode the simulated transaction data")
		}
		transactionData.Resources.Instructions = xdr.Uint32(
			uint64(transactionData.Resources.Instructions) * (100 + sorobanInstructionPadPct) / 100)
		transactionData.ResourceFee = xdr.Int64(
			int64(transactionData.ResourceFee) * (100 + sorobanResourceFeePadPct) / 100)
		resourceFee = int64(transactionData.ResourceFee)
		op.Ext = xdr.TransactionExt{
			V:           1,
			SorobanData: &transactionData,
		}
	}

	// Inclusion fee + padded resource fee; unused resource fee is refunded.
	fee := int64(txnbuild.MinBaseFee) + resourceFee

	tx, err := txnbuild.NewTransaction(txnbuild.TransactionParams{
		SourceAccount:        sourceAccount,
		IncrementSequenceNum: true,
		BaseFee:              fee,
		Preconditions: txnbuild.Preconditions{
			TimeBounds: txnbuild.NewTimeout(300),
		},
		Operations: []txnbuild.Operation{op},
	})
	if err != nil {
		return empty, errb.Code(pkgErrors.CodeBuildFailed).
			Wrapf(err, "could not build the contract transaction")
	}

	// Sign transaction
	tx, err = tx.Sign(s.networkPassphrase, signerKP)
	if err != nil {
		return empty, errb.Code(pkgErrors.CodeBuildFailed).
			Wrapf(err, "could not sign the contract transaction")
	}

	if onSigned != nil {
		txHash, err := tx.HashHex(s.networkPassphrase)
		if err != nil {
			return empty, errb.Code(pkgErrors.CodeBuildFailed).
				Wrapf(err, "could not hash the signed contract transaction")
		}
		validUntil := time.Unix(tx.Timebounds().MaxTime, 0)
		if err := onSigned(ctx, txHash, validUntil); err != nil {
			return empty, errb.Code(pkgErrors.CodeStateWriteFailed).With(pkgErrors.AttrTxHash, txHash).
				Wrapf(err, "signed transaction could not be recorded; not submitted")
		}
	}

	// Submit
	txXDR, _ := tx.Base64()
	sendResp, err := s.rpcClient.SendTransaction(ctx, protocol.SendTransactionRequest{
		Transaction: txXDR,
	})
	if err != nil {
		// The node may have accepted the transaction before the call failed.
		return empty, errb.Code(pkgErrors.CodeSubmitFailed).
			Wrapf(fmt.Errorf("%w: %w", types.ErrSubmissionUnconfirmed, err), "could not submit the contract transaction")
	}

	// ERROR and TRY_AGAIN_LATER mean stellar-core did not admit the
	// transaction, so it can never land; polling would only wait out the
	// timeout and report an outcome that is in fact known.
	switch sendResp.Status {
	case stellarcore.TXStatusError:
		return empty, rpc.RejectionError(errb, sendResp)
	case stellarcore.TXStatusTryAgainLater:
		return empty, errb.Code(pkgErrors.CodeSubmitFailed).With(pkgErrors.AttrTxHash, sendResp.Hash).
			Wrapf(types.ErrStellarCoreOverloaded, "stellar-core did not admit the contract transaction")
	}

	// Poll for result
	pollCfg := rpc.DefaultPollConfig()
	pollCfg.Logger = s.logger
	txResp, err := rpc.PollTransaction(ctx, s.rpcClient, sendResp.Hash, pollCfg)
	if err != nil {
		return txResp, err
	}

	if txResp.Status != protocol.TransactionStatusSuccess {
		return txResp, errb.
			Code(pkgErrors.CodeSubmitFailed).
			With(pkgErrors.AttrTxHash, sendResp.Hash).
			With("status", txResp.Status).
			Wrapf(types.ErrTransactionFailed, "transaction did not succeed on ledger")
	}

	// GetTransaction's response does not echo the hash it was queried with, so
	// PollTransaction cannot supply it. sendResp.Hash is the canonical hash from
	// submission — stamp it back on so callers return a real TxHash.
	txResp.TransactionHash = sendResp.Hash
	return txResp, nil
}

var invocationDuration, _ = telemetry.Meter().Float64Histogram("microvault.soroban.invocation.duration",
	metric.WithUnit("s"),
	metric.WithDescription("Duration of a signed Soroban contract invocation from build to confirmed result, by contract function and outcome."))

// invokeSigned builds, simulates and submits one signed contract call. errb
// supplies the caller's attributes so every failure below carries the same
// context without each call site restating it.
func (s *service) invokeSigned(
	ctx context.Context,
	signerKP *keypair.Full,
	fnName string,
	args []xdr.ScVal,
	errb oops.OopsErrorBuilder,
) (*protocol.GetTransactionResponse, error) {
	return s.invokeSignedHooked(ctx, signerKP, fnName, args, errb, nil)
}

// invokeSignedHooked is invokeSigned with a hook run between signing and
// submission.
func (s *service) invokeSignedHooked(
	ctx context.Context,
	signerKP *keypair.Full,
	fnName string,
	args []xdr.ScVal,
	errb oops.OopsErrorBuilder,
	onSigned signedHook,
) (*protocol.GetTransactionResponse, error) {
	ctx, span := telemetry.Tracer().Start(ctx, "soroban."+fnName,
		trace.WithAttributes(attribute.String(pkgErrors.AttrContractFunction, fnName)))
	defer span.End()
	start := time.Now()
	resp, err := s.invokeSignedTx(ctx, signerKP, fnName, args, errb, onSigned)
	invocationDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
		attribute.String(pkgErrors.AttrContractFunction, fnName), attribute.String("outcome", telemetry.Outcome(err))))
	if resp != nil {
		span.SetAttributes(attribute.String(pkgErrors.AttrTxHash, resp.TransactionHash))
	}
	telemetry.RecordError(span, err)
	return resp, err
}

// signedHook is types.RepayRequest.OnSigned as the submit path sees it.
type signedHook func(ctx context.Context, txHash string, validUntil time.Time) error

// invokeSignedTx is invokeSigned without the span.
func (s *service) invokeSignedTx(
	ctx context.Context,
	signerKP *keypair.Full,
	fnName string,
	args []xdr.ScVal,
	errb oops.OopsErrorBuilder,
	onSigned signedHook,
) (*protocol.GetTransactionResponse, error) {
	op, err := s.buildInvokeContractOp(fnName, args)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not build contract invocation")
	}

	simResp, err := s.simulateContractCall(ctx, signerKP.Address(), op)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeSimulationFailed).Wrapf(err, "contract simulation could not be performed")
	}

	// A simulation that returns an error string is a contract-level rejection,
	// not a transport failure. Wrapping the sentinel is what lets callers use
	// errors.Is rather than matching on the message.
	if simResp.Error != "" {
		s.logger.ErrorContext(ctx, "contract simulation rejected the call",
			"contract_function", fnName, "simulation_error", simResp.Error)
		return nil, errb.
			Code(pkgErrors.CodeSimulationRejected).
			With("simulation_error", simResp.Error).
			Wrapf(types.ErrSimulationFailed, "contract simulation rejected the call")
	}

	txResp, err := s.submitContractTransaction(ctx, signerKP, op, simResp, onSigned)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeSubmitFailed).Wrapf(err, "could not submit contract transaction")
	}

	return &txResp, nil
}
