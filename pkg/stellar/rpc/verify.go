package rpc

import (
	"context"
	"fmt"

	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
)

// Verifier confirms the on-ledger outcome of a transaction someone else
// claims to have submitted — an anchor reporting a refund, for example.
//
// Unlike PollTransaction it does not wait: a single lookup either answers
// definitively or reports the outcome as unknown, leaving the retry cadence to
// the caller.
type Verifier struct {
	client TransactionGetter
}

// NewVerifier returns a Verifier backed by the given RPC client.
func NewVerifier(client TransactionGetter) *Verifier {
	return &Verifier{client: client}
}

// TransactionSucceeded reports whether txHash succeeded on-ledger.
//
// The three outcomes are distinct and callers must treat them differently:
//   - (true, nil)   the transaction is on the ledger and succeeded
//   - (false, nil)  it is on the ledger and definitively failed
//   - (false, err)  unknown — not yet visible, outside the RPC's retention
//     window, or the lookup itself failed
//
// An unknown result is never a failure. Soroban RPC only retains recent
// transactions, so a hash older than the retention window reports NOT_FOUND
// indefinitely; callers that retry forever on error should bound their retries
// or escalate to a human.
func (v *Verifier) TransactionSucceeded(ctx context.Context, txHash string) (bool, error) {
	if txHash == "" {
		return false, fmt.Errorf("rpc: empty transaction hash")
	}

	resp, err := v.client.GetTransaction(ctx, protocol.GetTransactionRequest{Hash: txHash})
	if err != nil {
		return false, fmt.Errorf("rpc: get transaction %s: %w", txHash, err)
	}

	switch resp.Status {
	case protocol.TransactionStatusSuccess:
		return true, nil
	case protocol.TransactionStatusFailed:
		return false, nil
	case protocol.TransactionStatusNotFound:
		return false, fmt.Errorf("rpc: transaction %s not found on ledger", txHash)
	default:
		return false, fmt.Errorf("rpc: unexpected status %q for transaction %s", resp.Status, txHash)
	}
}
