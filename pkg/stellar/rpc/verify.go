package rpc

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"

	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Verifier confirms the on-ledger outcome of a transaction someone else
// claims to have submitted — an anchor reporting a refund, for example.
func verifyErr(op string) oops.OopsErrorBuilder {
	return oops.In(pkgErrors.DomainStellarClassic).Tags("rpc", "verify").With(pkgErrors.AttrOperation, op)
}

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
		return false, verifyErr("transaction_succeeded").Code(pkgErrors.CodeMissingAccount).Errorf("transaction hash is empty")
	}

	resp, err := v.client.GetTransaction(ctx, protocol.GetTransactionRequest{Hash: txHash})
	if err != nil {
		return false, verifyErr("transaction_succeeded").With(pkgErrors.AttrTxHash, txHash).
			Code(pkgErrors.CodeTransportFailed).Wrapf(err, "could not read the transaction")
	}

	switch resp.Status {
	case protocol.TransactionStatusSuccess:
		return true, nil
	case protocol.TransactionStatusFailed:
		return false, nil
	case protocol.TransactionStatusNotFound:
		return false, verifyErr("transaction_succeeded").With(pkgErrors.AttrTxHash, txHash).
			Code(pkgErrors.CodeNotFound).Errorf("transaction is not on the ledger")
	default:
		return false, verifyErr("transaction_succeeded").
			With(pkgErrors.AttrTxHash, txHash).
			With("status", resp.Status).
			Code(pkgErrors.CodeIncompleteResponse).
			Errorf("transaction has an unexpected status")
	}
}

// TxOutcome is what the ledger can say about a transaction we submitted.
type TxOutcome int

const (
	// TxUnresolved means the ledger cannot answer yet: the transaction may
	// still land before validUntil, or the latest ledger has not passed it.
	TxUnresolved TxOutcome = iota
	// TxSucceeded means the transaction is on the ledger and succeeded.
	TxSucceeded
	// TxFailed means the transaction is on the ledger and failed.
	TxFailed
	// TxNeverLanded means the RPC's history covers the whole validity window
	// and the transaction is not in it, so it can no longer be included.
	TxNeverLanded
	// TxOutsideRetention means the RPC no longer holds the ledgers the
	// transaction could have landed in; only an archive can answer.
	TxOutsideRetention
)

// TxResolution is the outcome of ResolveSubmitted and, on success, the ledger
// the transaction landed in.
type TxResolution struct {
	Outcome TxOutcome
	Ledger  uint32
}

// ResolveSubmitted settles the outcome of a transaction we signed and may have
// submitted. submittedAfter is any time at or before signing; validUntil is
// the transaction's max time bound. NOT_FOUND is only conclusive once the
// latest ledger has closed past validUntil and the oldest retained ledger
// closed before submittedAfter, both by ledger time rather than local clock.
func (v *Verifier) ResolveSubmitted(ctx context.Context, txHash string, submittedAfter, validUntil time.Time) (TxResolution, error) {
	if txHash == "" {
		return TxResolution{}, verifyErr("resolve_submitted").Code(pkgErrors.CodeMissingAccount).Errorf("transaction hash is empty")
	}

	resp, err := v.client.GetTransaction(ctx, protocol.GetTransactionRequest{Hash: txHash})
	if err != nil {
		return TxResolution{}, verifyErr("resolve_submitted").With(pkgErrors.AttrTxHash, txHash).
			Code(pkgErrors.CodeTransportFailed).Wrapf(err, "could not read the transaction")
	}

	switch resp.Status {
	case protocol.TransactionStatusSuccess:
		return TxResolution{Outcome: TxSucceeded, Ledger: resp.Ledger}, nil
	case protocol.TransactionStatusFailed:
		return TxResolution{Outcome: TxFailed, Ledger: resp.Ledger}, nil
	case protocol.TransactionStatusNotFound:
		switch {
		case resp.LatestLedgerCloseTime <= validUntil.Unix():
			return TxResolution{Outcome: TxUnresolved}, nil
		case resp.OldestLedgerCloseTime >= submittedAfter.Unix():
			return TxResolution{Outcome: TxOutsideRetention}, nil
		default:
			return TxResolution{Outcome: TxNeverLanded}, nil
		}
	default:
		return TxResolution{}, verifyErr("resolve_submitted").
			With(pkgErrors.AttrTxHash, txHash).
			With("status", resp.Status).
			Code(pkgErrors.CodeIncompleteResponse).
			Errorf("transaction has an unexpected status")
	}
}

// Payment is a classic payment operation extracted from a transaction envelope.
type Payment struct {
	Destination   string
	AssetCode     string
	AssetIssuer   string
	AmountStroops int64
}

// PaymentsTo returns the successful payments in txHash addressed to
// destination, in the named asset.
func (v *Verifier) PaymentsTo(ctx context.Context, txHash, destination, assetCode, assetIssuer string) ([]Payment, error) {
	if txHash == "" {
		return nil, verifyErr("payments_to").Code(pkgErrors.CodeMissingAccount).Errorf("transaction hash is empty")
	}
	if destination == "" {
		return nil, verifyErr("payments_to").Code(pkgErrors.CodeMissingAccount).Errorf("destination is empty")
	}

	resp, err := v.client.GetTransaction(ctx, protocol.GetTransactionRequest{
		Hash:   txHash,
		Format: protocol.FormatJSON,
	})
	if err != nil {
		return nil, verifyErr("payments_to").With(pkgErrors.AttrTxHash, txHash).
			Code(pkgErrors.CodeTransportFailed).Wrapf(err, "could not read the transaction")
	}
	switch resp.Status {
	case protocol.TransactionStatusSuccess:
	case protocol.TransactionStatusFailed:
		return nil, verifyErr("payments_to").With(pkgErrors.AttrTxHash, txHash).
			Code(pkgErrors.CodeSubmitFailed).Errorf("transaction failed on ledger")
	case protocol.TransactionStatusNotFound:
		return nil, verifyErr("payments_to").With(pkgErrors.AttrTxHash, txHash).
			Code(pkgErrors.CodeNotFound).Errorf("transaction is not on the ledger")
	default:
		return nil, verifyErr("payments_to").
			With(pkgErrors.AttrTxHash, txHash).
			With("status", resp.Status).
			Code(pkgErrors.CodeIncompleteResponse).
			Errorf("transaction has an unexpected status")
	}
	if len(resp.EnvelopeJSON) == 0 {
		return nil, verifyErr("payments_to").With(pkgErrors.AttrTxHash, txHash).
			Code(pkgErrors.CodeIncompleteResponse).Errorf("transaction has no envelope JSON")
	}

	var envelope any
	if err := json.Unmarshal(resp.EnvelopeJSON, &envelope); err != nil {
		return nil, verifyErr("payments_to").With(pkgErrors.AttrTxHash, txHash).
			Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the transaction envelope")
	}

	var out []Payment
	for _, p := range collectPayments(envelope) {
		if p.Destination != destination {
			continue
		}
		if assetCode != "" && p.AssetCode != assetCode {
			continue
		}
		if assetIssuer != "" && p.AssetIssuer != assetIssuer {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// collectPayments walks the decoded envelope for payment operation bodies.
func collectPayments(node any) []Payment {
	var out []Payment

	switch n := node.(type) {
	case map[string]any:
		if body, ok := n["payment"].(map[string]any); ok {
			if p, ok := parsePayment(body); ok {
				out = append(out, p)
			}
		}
		for _, v := range n {
			out = append(out, collectPayments(v)...)
		}
	case []any:
		for _, v := range n {
			out = append(out, collectPayments(v)...)
		}
	}
	return out
}

// parsePayment reads one payment body. Amounts are already stroops in the JSON
// envelope, so they are taken as integers — no float conversion.
func parsePayment(body map[string]any) (Payment, bool) {
	dest, _ := body["destination"].(string)
	rawAmount, _ := body["amount"].(string)
	if dest == "" || rawAmount == "" {
		return Payment{}, false
	}
	stroops, err := strconv.ParseInt(rawAmount, 10, 64)
	if err != nil {
		return Payment{}, false
	}

	code, issuer := "", ""
	if asset, ok := body["asset"].(map[string]any); ok {
		for _, key := range []string{"credit_alphanum4", "credit_alphanum12"} {
			if a, ok := asset[key].(map[string]any); ok {
				code, _ = a["asset_code"].(string)
				issuer, _ = a["issuer"].(string)
				break
			}
		}
		if code == "" {
			if _, ok := asset["native"]; ok {
				code = "XLM"
			}
		}
	}

	return Payment{Destination: dest, AssetCode: code, AssetIssuer: issuer, AmountStroops: stroops}, true
}
