package classic

import (
	"errors"
	"testing"

	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/types"
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

	r := decodeRejection(xdrStr)

	assert.Equal(t, "TxBadAuth", r.txCode)
	assert.True(t, r.permanent)
	assert.Empty(t, r.opCodes)
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

	r := decodeRejection(xdrStr)

	assert.Equal(t, "TxFailed", r.txCode)
	assert.True(t, r.permanent)
	assert.Equal(t, []string{
		"BeginSponsoringFutureReservesSuccess",
		"CreateAccountAlreadyExist",
	}, r.opCodes)
}

// A busy network must stay retryable, or a transient outage becomes a
// permanent failure with an ops alert.
func TestDecodeRejection_BadSeqIsTransient(t *testing.T) {
	xdrStr := mustResultXDR(t, xdr.TransactionResult{
		Result: xdr.TransactionResultResult{Code: xdr.TransactionResultCodeTxBadSeq},
	})

	r := decodeRejection(xdrStr)

	assert.Equal(t, "TxBadSeq", r.txCode)
	assert.False(t, r.permanent)
}

// An unreadable or absent result must not be written off as permanent.
func TestDecodeRejection_UndecodableIsTransient(t *testing.T) {
	for _, in := range []string{"", "not-base64-xdr"} {
		r := decodeRejection(in)
		assert.Empty(t, r.txCode)
		assert.False(t, r.permanent)
	}
}

func TestRejectionError_CarriesCodesAndWrapsSentinels(t *testing.T) {
	xdrStr := mustResultXDR(t, xdr.TransactionResult{
		Result: xdr.TransactionResultResult{Code: xdr.TransactionResultCodeTxBadAuth},
	})

	err := rejectionError("CreateSponsoredAccount", protocol.SendTransactionResponse{
		Hash:           "abc123",
		ErrorResultXDR: xdrStr,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, types.ErrTransactionRejected)
	assert.ErrorIs(t, err, types.ErrTransactionRejectedPermanent)

	var oopsErr interface{ Context() map[string]any }
	require.True(t, errors.As(err, &oopsErr))
	ctx := oopsErr.Context()
	assert.Equal(t, "TxBadAuth", ctx["tx_result_code"])
	assert.Equal(t, true, ctx["permanent"])
	assert.Equal(t, "abc123", ctx["tx_hash"])
}

// A transient rejection must not match the permanent sentinel, or the retry
// loop stops on a failure that would have cleared.
func TestRejectionError_TransientDoesNotMatchPermanent(t *testing.T) {
	xdrStr := mustResultXDR(t, xdr.TransactionResult{
		Result: xdr.TransactionResultResult{Code: xdr.TransactionResultCodeTxBadSeq},
	})

	err := rejectionError("CreateSponsoredAccount", protocol.SendTransactionResponse{ErrorResultXDR: xdrStr})

	assert.ErrorIs(t, err, types.ErrTransactionRejected)
	assert.NotErrorIs(t, err, types.ErrTransactionRejectedPermanent)
}
