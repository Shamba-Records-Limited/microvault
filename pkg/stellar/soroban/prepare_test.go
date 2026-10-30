package soroban

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stellar/go-stellar-sdk/keypair"
	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/txnbuild"
	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stellartesting "github.com/Shamba-Records-Limited/microvault/pkg/stellar/testing"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/types"
)

func newTestPreparer(m *stellartesting.MockRPCClient) (*Preparer, *stellartesting.TestKeys) {
	keys := stellartesting.NewTestKeys()
	admin := keypair.MustParseFull(keys.AdminSecret)
	return NewPreparer(m, stellartesting.TestNetworkPassphrase, admin.Address()), keys
}

func pauseCall(t *testing.T, contract, caller string) Invocation {
	t.Helper()
	addr, err := ScAddress(caller)
	require.NoError(t, err)
	return Invocation{Contract: contract, Function: "pause", Args: []xdr.ScVal{addr}}
}

func simulatesOK(m *stellartesting.MockRPCClient) {
	m.SimulateTransactionFunc = func(context.Context, protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
		return stellartesting.NewSimulationResponse().WithTransactionData().WithAuth().Build(), nil
	}
}

func TestPreparer_PrepareBuildsAnUnsignedEnvelopeForTheSource(t *testing.T) {
	m := stellartesting.NewMockRPCClient()
	simulatesOK(m)
	p, keys := newTestPreparer(m)
	guardian := keypair.MustParseFull(keys.UserSecret)

	prep, err := p.Prepare(context.Background(), guardian.Address(), pauseCall(t, keys.ContractID, guardian.Address()), 5*time.Minute)
	require.NoError(t, err)

	generic, err := txnbuild.TransactionFromXDR(prep.EnvelopeXDR)
	require.NoError(t, err)
	tx, ok := generic.Transaction()
	require.True(t, ok)
	assert.Equal(t, guardian.Address(), tx.SourceAccount().AccountID)
	assert.Empty(t, tx.Signatures())
	hash, err := tx.HashHex(stellartesting.TestNetworkPassphrase)
	require.NoError(t, err)
	assert.Equal(t, hash, prep.Hash)
	assert.Greater(t, prep.Fee, int64(txnbuild.MinBaseFee))
	assert.WithinDuration(t, time.Now().Add(5*time.Minute), prep.ValidUntil, 5*time.Second)
}

func TestPreparer_PrepareReportsTheContractsRefusal(t *testing.T) {
	m := stellartesting.NewMockRPCClient()
	m.SimulateTransactionFunc = func(context.Context, protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
		return stellartesting.NewSimulationResponse().WithError("Error(Contract, #1)").Build(), nil
	}
	p, keys := newTestPreparer(m)
	guardian := keypair.MustParseFull(keys.UserSecret)

	_, err := p.Prepare(context.Background(), guardian.Address(), pauseCall(t, keys.ContractID, guardian.Address()), time.Minute)
	var rejected *SimulationRejectedError
	require.ErrorAs(t, err, &rejected)
	assert.Equal(t, "Error(Contract, #1)", rejected.Reason)
	assert.ErrorIs(t, err, types.ErrSimulationFailed)
}

func TestPreparer_CheckSigned(t *testing.T) {
	m := stellartesting.NewMockRPCClient()
	simulatesOK(m)
	p, keys := newTestPreparer(m)
	guardian := keypair.MustParseFull(keys.UserSecret)
	ctx := context.Background()

	prep, err := p.Prepare(ctx, guardian.Address(), pauseCall(t, keys.ContractID, guardian.Address()), time.Minute)
	require.NoError(t, err)
	sign := func(envelope string) string {
		generic, err := txnbuild.TransactionFromXDR(envelope)
		require.NoError(t, err)
		tx, _ := generic.Transaction()
		tx, err = tx.Sign(stellartesting.TestNetworkPassphrase, guardian)
		require.NoError(t, err)
		out, err := tx.Base64()
		require.NoError(t, err)
		return out
	}

	t.Run("the prepared transaction, signed", func(t *testing.T) {
		assert.NoError(t, p.CheckSigned(sign(prep.EnvelopeXDR), prep.Hash))
	})
	t.Run("unsigned", func(t *testing.T) {
		assert.ErrorIs(t, p.CheckSigned(prep.EnvelopeXDR, prep.Hash), ErrUnsigned)
	})
	t.Run("a different transaction", func(t *testing.T) {
		other := pauseCall(t, keys.ContractID, guardian.Address())
		other.Function = "unpause"
		swapped, err := p.Prepare(ctx, guardian.Address(), other, time.Minute)
		require.NoError(t, err)
		assert.ErrorIs(t, p.CheckSigned(sign(swapped.EnvelopeXDR), prep.Hash), ErrEnvelopeMismatch)
	})
}

func TestPreparer_Submit(t *testing.T) {
	t.Run("confirmed", func(t *testing.T) {
		m := stellartesting.NewMockRPCClient()
		m.SendTransactionFunc = func(context.Context, protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
			return stellartesting.NewSendTransactionResponse().WithHash("abc").Build(), nil
		}
		m.GetTransactionFunc = func(context.Context, protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
			return stellartesting.NewGetTransactionResponse().WithStatus(protocol.TransactionStatusSuccess).Build(), nil
		}
		p, _ := newTestPreparer(m)
		resp, err := p.Submit(context.Background(), "AAAA", "abc")
		require.NoError(t, err)
		assert.Equal(t, "abc", resp.TransactionHash)
		assert.Equal(t, protocol.TransactionStatusSuccess, resp.Status)
	})
	t.Run("transport failure leaves the outcome unknown", func(t *testing.T) {
		m := stellartesting.NewMockRPCClient()
		m.SendTransactionFunc = func(context.Context, protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
			return protocol.SendTransactionResponse{}, errors.New("connection reset")
		}
		p, _ := newTestPreparer(m)
		_, err := p.Submit(context.Background(), "AAAA", "abc")
		assert.ErrorIs(t, err, types.ErrSubmissionUnconfirmed)
	})
}

func TestPreparer_ViewDecodesTheResult(t *testing.T) {
	m := stellartesting.NewMockRPCClient()
	m.SimulateTransactionFunc = func(context.Context, protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
		return stellartesting.NewSimulationResponse().WithBoolResult(true).Build(), nil
	}
	p, keys := newTestPreparer(m)
	v, err := p.View(context.Background(), Invocation{Contract: keys.ContractID, Function: "paused"})
	require.NoError(t, err)
	paused, err := DecodeBool(v)
	require.NoError(t, err)
	assert.True(t, paused)
}

func TestScI128RoundTrips(t *testing.T) {
	for _, s := range []string{"0", "240000000000000000", "-1", "170141183460469231731687303715884105727", "-170141183460469231731687303715884105728"} {
		want, err := types.ParseI128(s)
		require.NoError(t, err)
		got, err := DecodeI128(ScI128(want))
		require.NoError(t, err)
		assert.Equal(t, want.String(), got.String(), s)
	}
}

func TestDecodeEnum(t *testing.T) {
	got, err := DecodeEnum(ScVec(ScSymbol("Ready")))
	require.NoError(t, err)
	assert.Equal(t, "Ready", got)
	_, err = DecodeEnum(ScU32(1))
	assert.Error(t, err)
}

func TestEventsReadsEveryMatchingEvent(t *testing.T) {
	keys := stellartesting.NewTestKeys()
	vault := keys.ContractID
	other := strkey.MustEncode(strkey.VersionByteContract, make([]byte, 32))
	a, b := keypair.MustRandom().Address(), keypair.MustRandom().Address()
	event := func(contract, name, borrower string, amount int64) xdr.ContractEvent {
		raw, _ := strkey.Decode(strkey.VersionByteContract, contract)
		var id xdr.ContractId
		copy(id[:], raw)
		addr, _ := ScAddress(borrower)
		data := xdr.ScMap{{Key: ScSymbol("amount"), Val: ScI128(types.NewI128(amount))}}
		p := &data
		return xdr.ContractEvent{ContractId: &id, Type: xdr.ContractEventTypeContract, Body: xdr.ContractEventBody{V0: &xdr.ContractEventV0{
			Topics: []xdr.ScVal{ScSymbol(name), addr}, Data: xdr.ScVal{Type: xdr.ScValTypeScvMap, Map: &p},
		}}}
	}
	meta := xdr.TransactionMeta{V: 3, V3: &xdr.TransactionMetaV3{SorobanMeta: &xdr.SorobanTransactionMeta{
		Events: []xdr.ContractEvent{
			event(vault, "written_off", a, 100), event(other, "written_off", a, 999),
			event(vault, "repaid", a, 5), event(vault, "written_off", b, 250),
		},
		ReturnValue: ScVoid(),
	}}}
	encoded, err := xdr.MarshalBase64(meta)
	require.NoError(t, err)

	events, err := Events(encoded, vault, "written_off")
	require.NoError(t, err)
	require.Len(t, events, 2)
	got := map[string]string{}
	for _, ev := range events {
		borrower, err := DecodeAddress(ev.Topics[0])
		require.NoError(t, err)
		v, ok := ev.Field("amount")
		require.True(t, ok)
		amount, _ := DecodeI128(v)
		got[borrower] = amount.String()
	}
	assert.Equal(t, map[string]string{a: "100", b: "250"}, got)
}
