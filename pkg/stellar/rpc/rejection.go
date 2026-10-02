package rpc

import (
	"strings"

	"github.com/samber/lo"
	"github.com/samber/oops"
	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stellar/go-stellar-sdk/xdr"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/types"
)

// permanentTxCodes are the stellar-core rejections that resubmitting cannot
// clear. txBadSeq, txTooLate and txInsufficientFee are deliberately absent:
// both submit paths rebuild from a freshly loaded account on every attempt,
// so those do clear on their own.
var permanentTxCodes = map[xdr.TransactionResultCode]bool{
	xdr.TransactionResultCodeTxMalformed:           true,
	xdr.TransactionResultCodeTxBadAuth:             true,
	xdr.TransactionResultCodeTxBadAuthExtra:        true,
	xdr.TransactionResultCodeTxNoAccount:           true,
	xdr.TransactionResultCodeTxNotSupported:        true,
	xdr.TransactionResultCodeTxMissingOperation:    true,
	xdr.TransactionResultCodeTxInsufficientBalance: true,
	xdr.TransactionResultCodeTxFailed:              true,
}

// Rejection is the decoded form of stellar-core's refusal to admit a
// transaction. Absent codes mean the result XDR was missing or unreadable.
type Rejection struct {
	TxCode    string
	OpCodes   []string
	Permanent bool
}

// DecodeRejection reads stellar-core's base64 TransactionResult. A rejection
// with no decodable code is reported as transient so a genuine outage is
// retried rather than written off.
func DecodeRejection(resultXDR string) Rejection {
	if resultXDR == "" {
		return Rejection{}
	}

	var result xdr.TransactionResult
	if err := xdr.SafeUnmarshalBase64(resultXDR, &result); err != nil {
		return Rejection{}
	}

	code := result.Result.Code
	out := Rejection{
		TxCode:    trimEnumPrefix(code.String(), "TransactionResultCode"),
		Permanent: permanentTxCodes[code],
	}

	if opResults, ok := result.OperationResults(); ok {
		out.OpCodes = lo.Map(opResults, func(op xdr.OperationResult, _ int) string {
			return operationCode(op)
		})
	}
	return out
}

// operationCode names one operation's outcome. The outer code covers the
// envelope failures (opBadAuth, opNoAccount); opInner defers to the arm, whose
// own Code field carries the reason the operation itself failed.
func operationCode(op xdr.OperationResult) string {
	outer := trimEnumPrefix(op.Code.String(), "OperationResultCode")
	if op.Code != xdr.OperationResultCodeOpInner || op.Tr == nil {
		return outer
	}

	tr := *op.Tr
	switch tr.Type {
	case xdr.OperationTypeCreateAccount:
		return enumName(tr.MustCreateAccountResult().Code.String())
	case xdr.OperationTypeSetOptions:
		return enumName(tr.MustSetOptionsResult().Code.String())
	case xdr.OperationTypeBeginSponsoringFutureReserves:
		return enumName(tr.MustBeginSponsoringFutureReservesResult().Code.String())
	case xdr.OperationTypeEndSponsoringFutureReserves:
		return enumName(tr.MustEndSponsoringFutureReservesResult().Code.String())
	case xdr.OperationTypeChangeTrust:
		return enumName(tr.MustChangeTrustResult().Code.String())
	case xdr.OperationTypePayment:
		return enumName(tr.MustPaymentResult().Code.String())
	case xdr.OperationTypeInvokeHostFunction:
		return enumName(tr.MustInvokeHostFunctionResult().Code.String())
	default:
		// Every operation either submit path sends is named above; anything else
		// still reports its type so the gap is visible rather than silent.
		return outer + "(" + tr.Type.String() + ")"
	}
}

// enumName strips the generated Go type prefix from an XDR enum's String(),
// leaving the bare result name. The prefix is the text before the trailing
// "Code" segment, e.g. CreateAccountResultCodeCreateAccountAlreadyExist.
func enumName(s string) string {
	if i := strings.Index(s, "ResultCode"); i >= 0 {
		return s[i+len("ResultCode"):]
	}
	return s
}

// trimEnumPrefix removes a known generated prefix, leaving the bare code.
func trimEnumPrefix(s, prefix string) string {
	return strings.TrimPrefix(s, prefix)
}

// RejectionError builds the error for a stellar-core refusal on errb, carrying
// the decoded result codes as attributes so the log record names the cause.
// The returned error wraps ErrTransactionRejected, and additionally
// ErrTransactionRejectedPermanent when resubmission cannot help.
func RejectionError(errb oops.OopsErrorBuilder, resp protocol.SendTransactionResponse) error {
	r := DecodeRejection(resp.ErrorResultXDR)

	sentinel := types.ErrTransactionRejected
	if r.Permanent {
		sentinel = types.ErrTransactionRejectedPermanent
	}

	errb = errb.
		Code(pkgErrors.CodeTransactionRejected).
		With("tx_hash", resp.Hash).
		With("tx_result_code", lo.CoalesceOrEmpty(r.TxCode, "undecodable")).
		With("permanent", r.Permanent)

	if len(r.OpCodes) > 0 {
		errb = errb.With("op_result_codes", r.OpCodes)
	}
	if r.Permanent {
		errb = errb.Hint("The envelope itself is wrong — signatures, operations " +
			"or source account. Resubmitting builds the same one. Nothing reached " +
			"a ledger, so the hash will not resolve on any explorer.")
	}
	if r.TxCode == "" && resp.ErrorResultXDR != "" {
		errb = errb.With("error_result_xdr", resp.ErrorResultXDR)
	}

	return errb.Wrap(sentinel)
}
