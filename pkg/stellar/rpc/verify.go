package rpc

import (
	"context"
	"encoding/json"
	"strconv"

	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"

	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Verifier confirms the on-ledger outcome of a transaction someone else
// claims to have submitted — an anchor reporting a refund, for example.
//
// verifyErr starts an error builder for on-ledger verification. The
// transaction hash is the attribute that matters: every failure here is
// diagnosed by looking the transaction up.
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

// Payment is a classic payment operation extracted from a transaction envelope.
type Payment struct {
	Destination   string
	AssetCode     string
	AssetIssuer   string
	AmountStroops int64
}

// PaymentsTo returns the successful payments in txHash addressed to
// destination, in the named asset.
//
// assetIssuer, when non-empty, must also match. Asset codes are not unique —
// anyone can issue an asset called USDC — so matching on code alone would let
// a worthless look-alike settle a refund.
//
// This exists because "the transaction succeeded" is not the question worth
// asking about an anchor's claimed refund — our own outbound payment to that
// anchor also succeeded. What matters is that value moved in the right
// direction, in the right asset, and how much. Reading it from the ledger
// means a mislabelled or misread anchor field cannot drive a vault repay.
//
// Returns an error unless the transaction is on-ledger and succeeded.
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
//
// The walk is recursive rather than indexed because the nesting differs by
// envelope type — a fee-bump buries the operations under
// tx_fee_bump.tx.inner_tx.tx.tx, and MoneyGram's refunds arrive fee-bumped.
// Searching by shape survives that without a branch per envelope variant.
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
