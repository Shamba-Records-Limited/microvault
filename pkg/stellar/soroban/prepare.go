package soroban

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/samber/oops"
	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stellar/go-stellar-sdk/protocols/stellarcore"
	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/txnbuild"
	"github.com/stellar/go-stellar-sdk/xdr"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/rpc"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/types"
)

// ErrEnvelopeMismatch is returned when a signed envelope is not the
// transaction that was prepared.
var ErrEnvelopeMismatch = errors.New("signed transaction differs from the prepared one")

// ErrUnsigned is returned when an envelope carries no signature.
var ErrUnsigned = errors.New("transaction is not signed")

// Invocation is one contract call.
type Invocation struct {
	Contract string
	Function string
	Args     []xdr.ScVal
}

// Prepared is a simulated, unsigned transaction for a wallet to sign.
type Prepared struct {
	EnvelopeXDR string
	Hash        string
	Source      string
	Fee         int64
	ValidUntil  time.Time
}

// Preparer builds contract calls for keys the server does not hold: it
// prepares them for a wallet, and submits and tracks what the wallet signed.
type Preparer struct {
	svc        *service
	viewSource string
}

// NewPreparer creates a Preparer. viewSource is any existing account, used as
// the simulation source for views.
func NewPreparer(rpcClient RPCClient, networkPassphrase, viewSource string) *Preparer {
	return &Preparer{
		svc: &service{
			rpcClient:         rpcClient,
			networkPassphrase: networkPassphrase,
			logger:            slog.Default().With(slog.String("service", "soroban_preparer")),
		},
		viewSource: viewSource,
	}
}

func prepErr(op string) oops.OopsErrorBuilder {
	return oops.In(errDomain).Tags("soroban", "wallet").With(pkgErrors.AttrOperation, op)
}

func invokeOp(inv Invocation) (*txnbuild.InvokeHostFunction, error) {
	raw, err := strkey.Decode(strkey.VersionByteContract, inv.Contract)
	if err != nil {
		return nil, fmt.Errorf("contract id %q: %w", inv.Contract, err)
	}
	var id xdr.ContractId
	copy(id[:], raw)
	addr, err := xdr.NewScAddress(xdr.ScAddressTypeScAddressTypeContract, id)
	if err != nil {
		return nil, err
	}
	return &txnbuild.InvokeHostFunction{
		HostFunction: xdr.HostFunction{
			Type: xdr.HostFunctionTypeHostFunctionTypeInvokeContract,
			InvokeContract: &xdr.InvokeContractArgs{
				ContractAddress: addr,
				FunctionName:    xdr.ScSymbol(inv.Function),
				Args:            inv.Args,
			},
		},
	}, nil
}

// SimulationRejectedError carries the contract's reason for refusing a call.
type SimulationRejectedError struct{ Reason string }

func (e *SimulationRejectedError) Error() string { return "contract rejected the call: " + e.Reason }

func (e *SimulationRejectedError) Unwrap() error { return types.ErrSimulationFailed }

// Prepare simulates inv with source as the transaction source and returns the
// unsigned envelope, valid for ttl.
func (p *Preparer) Prepare(ctx context.Context, source string, inv Invocation, ttl time.Duration) (*Prepared, error) {
	errb := prepErr("prepare").With(pkgErrors.AttrAddress, source).With(pkgErrors.AttrContractFunction, inv.Function)
	op, err := invokeOp(inv)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not build the contract call")
	}
	simResp, err := p.svc.simulateContractCall(ctx, source, op)
	if err != nil {
		return nil, err
	}
	if simResp.Error != "" {
		return nil, errb.Code(pkgErrors.CodeSimulationRejected).Wrap(&SimulationRejectedError{Reason: simResp.Error})
	}
	fee, err := applySimulation(op, simResp)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not apply the simulation")
	}
	account, err := p.svc.rpcClient.LoadAccount(ctx, source)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeTransportFailed).Wrapf(err, "could not load the source account")
	}
	bounds := txnbuild.NewTimeout(int64(ttl.Seconds()))
	tx, err := txnbuild.NewTransaction(txnbuild.TransactionParams{
		SourceAccount:        account,
		IncrementSequenceNum: true,
		BaseFee:              fee,
		Preconditions:        txnbuild.Preconditions{TimeBounds: bounds},
		Operations:           []txnbuild.Operation{op},
	})
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not build the transaction")
	}
	envelope, err := tx.Base64()
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeEncodeFailed).Wrapf(err, "could not encode the transaction")
	}
	hash, err := tx.HashHex(p.svc.networkPassphrase)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not hash the transaction")
	}
	return &Prepared{
		EnvelopeXDR: envelope,
		Hash:        hash,
		Source:      source,
		Fee:         tx.BaseFee(),
		ValidUntil:  time.Unix(tx.Timebounds().MaxTime, 0),
	}, nil
}

// View simulates a read-only call and returns its value.
func (p *Preparer) View(ctx context.Context, inv Invocation) (xdr.ScVal, error) {
	errb := prepErr("view").With(pkgErrors.AttrContractFunction, inv.Function)
	op, err := invokeOp(inv)
	if err != nil {
		return xdr.ScVal{}, errb.Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not build the contract call")
	}
	simResp, err := p.svc.simulateContractCall(ctx, p.viewSource, op)
	if err != nil {
		return xdr.ScVal{}, err
	}
	if simResp.Error != "" {
		return xdr.ScVal{}, errb.Code(pkgErrors.CodeSimulationRejected).Wrap(&SimulationRejectedError{Reason: simResp.Error})
	}
	if len(simResp.Results) == 0 || simResp.Results[0].ReturnValueXDR == nil {
		return xdr.ScVal{}, errb.Code(pkgErrors.CodeIncompleteResponse).Wrapf(types.ErrNoSimulationResult, "simulation returned no value")
	}
	var out xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(*simResp.Results[0].ReturnValueXDR, &out); err != nil {
		return xdr.ScVal{}, errb.Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the simulation result")
	}
	return out, nil
}

// CheckSigned confirms signedXDR is the prepared transaction (same hash) and
// carries at least one signature.
func (p *Preparer) CheckSigned(signedXDR, wantHash string) error {
	generic, err := txnbuild.TransactionFromXDR(signedXDR)
	if err != nil {
		return fmt.Errorf("decode signed transaction: %w", err)
	}
	tx, ok := generic.Transaction()
	if !ok {
		return fmt.Errorf("fee-bump envelopes are not accepted: %w", ErrEnvelopeMismatch)
	}
	hash, err := tx.HashHex(p.svc.networkPassphrase)
	if err != nil {
		return fmt.Errorf("hash signed transaction: %w", err)
	}
	if hash != wantHash {
		return ErrEnvelopeMismatch
	}
	if len(tx.Signatures()) == 0 {
		return ErrUnsigned
	}
	return nil
}

// Submit sends a signed envelope that CheckSigned accepted and waits for its
// result. An error wrapping types.ErrSubmissionUnconfirmed means the outcome
// is unknown and must be read back with Lookup.
func (p *Preparer) Submit(ctx context.Context, signedXDR, hash string) (protocol.GetTransactionResponse, error) {
	errb := prepErr("submit").With(pkgErrors.AttrTxHash, hash)
	sendResp, err := p.svc.rpcClient.SendTransaction(ctx, protocol.SendTransactionRequest{Transaction: signedXDR})
	if err != nil {
		return protocol.GetTransactionResponse{}, errb.Code(pkgErrors.CodeSubmitFailed).
			Wrapf(fmt.Errorf("%w: %w", types.ErrSubmissionUnconfirmed, err), "could not submit the transaction")
	}
	switch sendResp.Status {
	case stellarcore.TXStatusError:
		return protocol.GetTransactionResponse{}, rpc.RejectionError(errb, sendResp)
	case stellarcore.TXStatusTryAgainLater:
		return protocol.GetTransactionResponse{}, errb.Code(pkgErrors.CodeSubmitFailed).
			Wrapf(types.ErrStellarCoreOverloaded, "stellar-core did not admit the transaction")
	}
	cfg := rpc.DefaultPollConfig()
	cfg.Logger = p.svc.logger
	resp, err := rpc.PollTransaction(ctx, p.svc.rpcClient, sendResp.Hash, cfg)
	if err != nil {
		return resp, errb.Code(pkgErrors.CodeSubmitFailed).
			Wrapf(fmt.Errorf("%w: %w", types.ErrSubmissionUnconfirmed, err), "the transaction result was not read")
	}
	resp.TransactionHash = sendResp.Hash
	return resp, nil
}

// Lookup reads a transaction's outcome by hash. NOT_FOUND means the network
// has not seen it, or not yet.
func (p *Preparer) Lookup(ctx context.Context, hash string) (protocol.GetTransactionResponse, error) {
	resp, err := p.svc.rpcClient.GetTransaction(ctx, protocol.GetTransactionRequest{Hash: hash})
	if err != nil {
		return resp, prepErr("lookup").With(pkgErrors.AttrTxHash, hash).Code(pkgErrors.CodeTransportFailed).
			Wrapf(err, "could not read the transaction")
	}
	return resp, nil
}

// ReturnValue decodes the invoked function's return value from a confirmed
// transaction.
func ReturnValue(resp protocol.GetTransactionResponse) (xdr.ScVal, error) {
	return extractReturnValue(resp.ResultMetaXDR)
}
