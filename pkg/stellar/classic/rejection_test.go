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
