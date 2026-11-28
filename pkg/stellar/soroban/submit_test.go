package soroban

import (
	"context"
	"testing"

	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stellar/go-stellar-sdk/protocols/stellarcore"
	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stellartesting "github.com/Shamba-Records-Limited/microvault/pkg/stellar/testing"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/types"
)

func resultXDR(t *testing.T, code xdr.TransactionResultCode) string {
	t.Helper()
	b64, err := xdr.MarshalBase64(xdr.TransactionResult{Result: xdr.TransactionResultResult{Code: code}})
	require.NoError(t, err)
	return b64
}

// borrowMock rejects the first rejections sends with code, then accepts.
func borrowMock(t *testing.T, rejections int, code xdr.TransactionResultCode) (client *stellartesting.MockRPCClient, sends *int) {
	t.Helper()
	m := stellartesting.NewMockRPCClient()
	m.SimulateTransactionFunc = func(context.Context, protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
		return stellartesting.NewSimulationResponse().WithI128Result(1).WithTransactionData().WithAuth().WithMinResourceFee(10000).Build(), nil
	}
	n := 0
	m.SendTransactionFunc = func(context.Context, protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
		n++
		if n <= rejections {
			return stellartesting.NewSendTransactionResponse().WithStatus(stellarcore.TXStatusError).WithError(resultXDR(t, code)).Build(), nil
		}
		return stellartesting.NewSendTransactionResponse().WithHash("borrow_tx").Build(), nil
	}
	m.GetTransactionFunc = func(context.Context, protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
		return stellartesting.NewGetTransactionResponse().WithStatus(protocol.TransactionStatusSuccess).Build(), nil
	}
	return m, &n
}

func borrow(svc Service) error {
	_, err := svc.BorrowFromVault(context.Background(), types.BorrowRequest{RecipientAddress: stellartesting.NewTestKeys().UserPublic, Amount: 1})
	return err
}

func TestSubmit_ResubmitsAfterBadSeq(t *testing.T) {
	m, sends := borrowMock(t, 2, xdr.TransactionResultCodeTxBadSeq)
	require.NoError(t, borrow(newTestService(m)))
	assert.Equal(t, 3, *sends)
	assert.Len(t, m.LoadAccountCalls, 3, "the sequence number is reloaded for each attempt")
}

func TestSubmit_GivesUpAfterRepeatedBadSeq(t *testing.T) {
	m, sends := borrowMock(t, 10, xdr.TransactionResultCodeTxBadSeq)
	require.Error(t, borrow(newTestService(m)))
	assert.Equal(t, maxBadSeqAttempts, *sends)
}

func TestSubmit_OtherRejectionsAreNotRetried(t *testing.T) {
	m, sends := borrowMock(t, 10, xdr.TransactionResultCodeTxInsufficientBalance)
	require.Error(t, borrow(newTestService(m)))
	assert.Equal(t, 1, *sends)
}
