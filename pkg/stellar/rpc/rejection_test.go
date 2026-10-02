package rpc

import (
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustResultXDR(t *testing.T, result xdr.TransactionResult) string {
	t.Helper()
	encoded, err := xdr.MarshalBase64(result)
	require.NoError(t, err)
	return encoded
}

// The staging failure: the child account already exists with its master key at
// weight 0, so its own signature no longer satisfies the high threshold the
// SetOptions operations need. stellar-core refuses the envelope outright.
func TestDecodeRejection_BadAuthIsPermanent(t *testing.T) {
	xdrStr := mustResultXDR(t, xdr.TransactionResult{
		Result: xdr.TransactionResultResult{Code: xdr.TransactionResultCodeTxBadAuth},
	})

	r := DecodeRejection(xdrStr)

	assert.Equal(t, "TxBadAuth", r.TxCode)
	assert.True(t, r.Permanent)
	assert.Empty(t, r.OpCodes)
}

// An operation-level failure names the operation that failed, not just txFailed.
func TestDecodeRejection_NamesTheFailingOperation(t *testing.T) {
	results := []xdr.OperationResult{
		{
			Code: xdr.OperationResultCodeOpInner,
			Tr: &xdr.OperationResultTr{
				Type: xdr.OperationTypeBeginSponsoringFutureReserves,
				BeginSponsoringFutureReservesResult: &xdr.BeginSponsoringFutureReservesResult{
					Code: xdr.BeginSponsoringFutureReservesResultCodeBeginSponsoringFutureReservesSuccess,
				},
			},
		},
		{
			Code: xdr.OperationResultCodeOpInner,
			Tr: &xdr.OperationResultTr{
				Type: xdr.OperationTypeCreateAccount,
				CreateAccountResult: &xdr.CreateAccountResult{
					Code: xdr.CreateAccountResultCodeCreateAccountAlreadyExist,
				},
			},
		},
	}
	xdrStr := mustResultXDR(t, xdr.TransactionResult{
		Result: xdr.TransactionResultResult{
			Code:    xdr.TransactionResultCodeTxFailed,
			Results: &results,
		},
	})

	r := DecodeRejection(xdrStr)

	assert.Equal(t, "TxFailed", r.TxCode)
	assert.True(t, r.Permanent)
	assert.Equal(t, []string{
		"BeginSponsoringFutureReservesSuccess",
		"CreateAccountAlreadyExist",
	}, r.OpCodes)
}

// A busy network must stay retryable, or a transient outage becomes a
// permanent failure with an ops alert.
func TestDecodeRejection_BadSeqIsTransient(t *testing.T) {
	xdrStr := mustResultXDR(t, xdr.TransactionResult{
		Result: xdr.TransactionResultResult{Code: xdr.TransactionResultCodeTxBadSeq},
	})

	r := DecodeRejection(xdrStr)

	assert.Equal(t, "TxBadSeq", r.TxCode)
	assert.False(t, r.Permanent)
}

// An unreadable or absent result must not be written off as permanent.
func TestDecodeRejection_UndecodableIsTransient(t *testing.T) {
	for _, in := range []string{"", "not-base64-xdr"} {
		r := DecodeRejection(in)
		assert.Empty(t, r.TxCode)
		assert.False(t, r.Permanent)
	}
}

func TestDecodeRejection_NamesAFailedContractInvocation(t *testing.T) {
	results := []xdr.OperationResult{{
		Code: xdr.OperationResultCodeOpInner,
		Tr: &xdr.OperationResultTr{
			Type: xdr.OperationTypeInvokeHostFunction,
			InvokeHostFunctionResult: &xdr.InvokeHostFunctionResult{
				Code: xdr.InvokeHostFunctionResultCodeInvokeHostFunctionTrapped,
			},
		},
	}}
	xdrStr := mustResultXDR(t, xdr.TransactionResult{
		Result: xdr.TransactionResultResult{Code: xdr.TransactionResultCodeTxFailed, Results: &results},
	})

	assert.Equal(t, []string{"InvokeHostFunctionTrapped"}, DecodeRejection(xdrStr).OpCodes)
}
