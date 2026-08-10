package rpc

import (
	"context"
	"encoding/json"
	"testing"

	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Real MoneyGram refund envelope: fee-bumped, so the payment operation sits
// under tx_fee_bump.tx.inner_tx.tx.tx rather than at the top level.
const mgRefundEnvelope = `{"tx_fee_bump":{"tx":{"fee_source":"GAYF33NNNMI2Z6VNRFXQ64D4E4SF77PM46NW3ZUZEEU5X7FCHAZCMHKU","fee":"90950","inner_tx":{"tx":{"tx":{"source_account":"GAYF33NNNMI2Z6VNRFXQ64D4E4SF77PM46NW3ZUZEEU5X7FCHAZCMHKU","memo":{"id":"8957218622196304938"},"operations":[{"source_account":"GAYF33NNNMI2Z6VNRFXQ64D4E4SF77PM46NW3ZUZEEU5X7FCHAZCMHKU","body":{"payment":{"destination":"GCU7KVTYEPCUZOQPJ6RLNWW64FS3IBHGZTEFWERO7ZZVDQYHMG7W5B6M","asset":{"credit_alphanum4":{"asset_code":"USDC","issuer":"GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5"}},"amount":"78000000"}}}]}}},"ext":"v0"}}}`

const (
	ourWallet = "GCU7KVTYEPCUZOQPJ6RLNWW64FS3IBHGZTEFWERO7ZZVDQYHMG7W5B6M"
	mgWallet  = "GAYF33NNNMI2Z6VNRFXQ64D4E4SF77PM46NW3ZUZEEU5X7FCHAZCMHKU"
)

// envelopeGetter mirrors the real server on the point that matters: the
// envelope is only returned as JSON when the request asks for it. Soroban RPC
// defaults to base64 XDR, so a fake that always hands back EnvelopeJSON hides
// a caller that forgot the format.
type envelopeGetter struct {
	status   string
	envelope string
	format   string
}

func (s *envelopeGetter) GetTransaction(_ context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
	s.format = req.Format
	details := protocol.TransactionDetails{Status: s.status}
	if req.Format == protocol.FormatJSON {
		details.EnvelopeJSON = json.RawMessage(s.envelope)
	}
	return protocol.GetTransactionResponse{TransactionDetails: details}, nil
}

func TestPaymentsTo_ReadsFeeBumpedRefund(t *testing.T) {
	v := NewVerifier(&envelopeGetter{status: protocol.TransactionStatusSuccess, envelope: mgRefundEnvelope})

	got, err := v.PaymentsTo(context.Background(), "abc", ourWallet, "USDC", "")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, int64(78000000), got[0].AmountStroops)
	assert.Equal(t, "USDC", got[0].AssetCode)
}

// The check that matters: our own outbound send to the anchor also "succeeds",
// so direction is what separates a refund from the payment it refunds.
func TestPaymentsTo_IgnoresWrongDirection(t *testing.T) {
	v := NewVerifier(&envelopeGetter{status: protocol.TransactionStatusSuccess, envelope: mgRefundEnvelope})

	got, err := v.PaymentsTo(context.Background(), "abc", mgWallet, "USDC", "")
	require.NoError(t, err)
	assert.Empty(t, got, "a payment to the anchor must never count as a refund")
}

func TestPaymentsTo_IgnoresWrongAsset(t *testing.T) {
	v := NewVerifier(&envelopeGetter{status: protocol.TransactionStatusSuccess, envelope: mgRefundEnvelope})

	got, err := v.PaymentsTo(context.Background(), "abc", ourWallet, "EURC", "")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestPaymentsTo_RejectsFailedAndMissing(t *testing.T) {
	for _, status := range []string{protocol.TransactionStatusFailed, protocol.TransactionStatusNotFound} {
		v := NewVerifier(&envelopeGetter{status: status, envelope: mgRefundEnvelope})
		_, err := v.PaymentsTo(context.Background(), "abc", ourWallet, "USDC", "")
		require.Error(t, err, "status %s must not yield payments", status)
	}
}

// Soroban RPC returns base64 XDR unless the request asks for JSON, so omitting
// the format leaves EnvelopeJSON empty against a real node while any fake that
// ignores the field still passes.
func TestPaymentsTo_RequestsJSONFormat(t *testing.T) {
	getter := &envelopeGetter{status: protocol.TransactionStatusSuccess, envelope: mgRefundEnvelope}
	v := NewVerifier(getter)

	_, err := v.PaymentsTo(context.Background(), "abc", ourWallet, "USDC", "")
	require.NoError(t, err)
	assert.Equal(t, protocol.FormatJSON, getter.format)
}

// Asset codes are not unique. A refund must arrive in USDC from the issuer we
// actually hold, or a worthless look-alike could settle a loan and drive a
// vault repay.
func TestPaymentsTo_RequiresMatchingIssuer(t *testing.T) {
	const realIssuer = "GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5"
	v := NewVerifier(&envelopeGetter{status: protocol.TransactionStatusSuccess, envelope: mgRefundEnvelope})

	got, err := v.PaymentsTo(context.Background(), "abc", ourWallet, "USDC", realIssuer)
	require.NoError(t, err)
	require.Len(t, got, 1, "the genuine issuer must still match")
	assert.Equal(t, realIssuer, got[0].AssetIssuer)

	impostor := "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF"
	got, err = v.PaymentsTo(context.Background(), "abc", ourWallet, "USDC", impostor)
	require.NoError(t, err)
	assert.Empty(t, got, "a same-code asset from another issuer must not count as a refund")
}
