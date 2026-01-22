package soroban

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/stellar/go-stellar-sdk/clients/rpcclient"
	"github.com/stellar/go-stellar-sdk/keypair"
	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/txnbuild"
	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/Shamba-Records-Limited/Microvault/internal/core/services/stellar/rpc"
	"github.com/Shamba-Records-Limited/Microvault/internal/core/services/stellar/types"
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
	IsUserLocked(ctx context.Context, userAddress string) (bool, error)
	GetLockPeriod(ctx context.Context) (uint64, error)
	GetRemainingLockTime(ctx context.Context, userAddress string) (uint64, error)
	IsPaused(ctx context.Context) (bool, error)

	// Treasury Operations
	BorrowFromVault(ctx context.Context, req types.BorrowRequest) (*types.BorrowResponse, error)
	RepayToVault(ctx context.Context, req types.RepayRequest) (*types.RepayResponse, error)
	AccrueInterest(ctx context.Context) error

	// Admin Operations
	PauseVault(ctx context.Context) error
	UnpauseVault(ctx context.Context) error
	SetMaxDeposit(ctx context.Context, limit int64) error
	SetMaxWithdraw(ctx context.Context, limit int64) error
	SetLockPeriod(ctx context.Context, periodSeconds uint64) error
}

type service struct {
	rpcClient          RPCClient
	networkPassphrase  string
	treasuryPrivateKey string
	adminPrivateKey    string
	contractID         string
	logger             *slog.Logger
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

// getContractAddress parses the contract ID and returns an ScAddress
func (s *service) getContractAddress() (xdr.ScAddress, error) {
	contractBytes, err := strkey.Decode(strkey.VersionByteContract, s.contractID)
	if err != nil {
		return xdr.ScAddress{}, fmt.Errorf("invalid contract ID: %w", err)
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
	sourceAccount, err := s.rpcClient.LoadAccount(ctx, sourceAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to load source account: %w", err)
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
		return nil, fmt.Errorf("failed to build transaction: %w", err)
	}

	txXDR, err := tx.Base64()
	if err != nil {
		return nil, fmt.Errorf("failed to encode transaction: %w", err)
	}

	simResp, err := s.rpcClient.SimulateTransaction(ctx, protocol.SimulateTransactionRequest{
		Transaction: txXDR,
	})
	if err != nil {
		return nil, fmt.Errorf("simulation failed: %w", err)
	}

	return &simResp, nil
}

// submitContractTransaction submits a signed contract transaction
func (s *service) submitContractTransaction(
	ctx context.Context,
	signerKP *keypair.Full,
	op *txnbuild.InvokeHostFunction,
	simResp *protocol.SimulateTransactionResponse,
) (string, error) {
	sourceAccount, err := s.rpcClient.LoadAccount(ctx, signerKP.Address())
	if err != nil {
		return "", fmt.Errorf("failed to load source account: %w", err)
	}

	// Extract auth entries from simulation results
	var authEntries []xdr.SorobanAuthorizationEntry
	if len(simResp.Results) > 0 && simResp.Results[0].AuthXDR != nil {
		for _, authXDR := range *simResp.Results[0].AuthXDR {
			var auth xdr.SorobanAuthorizationEntry
			if err := xdr.SafeUnmarshalBase64(authXDR, &auth); err != nil {
				return "", fmt.Errorf("failed to decode auth: %w", err)
			}
			authEntries = append(authEntries, auth)
		}
	}

	// Set auth from simulation
	op.Auth = authEntries

	// Calculate fee (base fee + resource fee from simulation)
	fee := int64(txnbuild.MinBaseFee)
	if simResp.MinResourceFee > 0 {
		fee += simResp.MinResourceFee
	}

	// Parse and set Soroban transaction data on the operation
	if simResp.TransactionDataXDR != "" {
		var transactionData xdr.SorobanTransactionData
		if err := xdr.SafeUnmarshalBase64(simResp.TransactionDataXDR, &transactionData); err != nil {
			return "", fmt.Errorf("failed to decode transaction data: %w", err)
		}
		op.Ext = xdr.TransactionExt{
			V:           1,
			SorobanData: &transactionData,
		}
	}

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
		return "", fmt.Errorf("failed to build transaction: %w", err)
	}

	// Sign transaction
	tx, err = tx.Sign(s.networkPassphrase, signerKP)
	if err != nil {
		return "", fmt.Errorf("failed to sign transaction: %w", err)
	}

	// Submit
	txXDR, _ := tx.Base64()
	resp, err := s.rpcClient.SendTransaction(ctx, protocol.SendTransactionRequest{
		Transaction: txXDR,
	})
	if err != nil {
		return "", fmt.Errorf("failed to submit transaction: %w", err)
	}

	// Poll for result
	pollCfg := rpc.DefaultPollConfig()
	pollCfg.Logger = s.logger
	status, err := rpc.PollTransaction(ctx, s.rpcClient, resp.Hash, pollCfg)
	if err != nil {
		return "", err
	}

	if status != protocol.TransactionStatusSuccess {
		return "", fmt.Errorf("transaction failed with status: %s", status)
	}

	return resp.Hash, nil
}
