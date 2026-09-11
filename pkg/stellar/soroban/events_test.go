package soroban

import (
	"testing"

	"github.com/stellar/go-stellar-sdk/keypair"
	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	testAddrA = keypair.MustRandom().Address()
	testAddrB = keypair.MustRandom().Address()
	testAddrC = keypair.MustRandom().Address()
)

// topicXDR encodes a symbol name followed by zero or more addresses into the
// base64 XDR topic strings a getEvents response would carry.
func topicXDR(t *testing.T, symbol string, addresses ...string) []string {
	t.Helper()
	topics := []string{encodeSymbol(t, symbol)}
	for _, addr := range addresses {
		topics = append(topics, encodeAddress(t, addr))
	}
	return topics
}

func encodeSymbol(t *testing.T, s string) string {
	t.Helper()
	sym := xdr.ScSymbol(s)
	val := xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &sym}
	out, err := xdr.MarshalBase64(val)
	require.NoError(t, err)
	return out
}

func encodeAddress(t *testing.T, addr string) string {
	t.Helper()
	val, err := addressToScVal(addr)
	require.NoError(t, err)
	out, err := xdr.MarshalBase64(val)
	require.NoError(t, err)
	return out
}

func TestDecodeVaultEvent(t *testing.T) {
	t.Run("deposit event yields operator, from, receiver in order", func(t *testing.T) {
		info := protocol.EventInfo{
			ID:       "deposit-1",
			TopicXDR: topicXDR(t, "deposit", testAddrA, testAddrB, testAddrC),
		}
		event, err := DecodeVaultEvent(info)
		require.NoError(t, err)
		assert.Equal(t, VaultEventDeposit, event.Kind)
		assert.Equal(t, []string{testAddrA, testAddrB, testAddrC}, event.Addresses)
	})

	t.Run("transfer event yields from, to", func(t *testing.T) {
		info := protocol.EventInfo{
			ID:       "transfer-1",
			TopicXDR: topicXDR(t, "transfer", testAddrA, testAddrB),
		}
		event, err := DecodeVaultEvent(info)
		require.NoError(t, err)
		assert.Equal(t, VaultEventTransfer, event.Kind)
		assert.Equal(t, []string{testAddrA, testAddrB}, event.Addresses)
	})

	t.Run("user_allowed event yields the single user address", func(t *testing.T) {
		info := protocol.EventInfo{
			ID:       "allow-1",
			TopicXDR: topicXDR(t, "user_allowed", testAddrA),
		}
		event, err := DecodeVaultEvent(info)
		require.NoError(t, err)
		assert.Equal(t, VaultEventUserAllowed, event.Kind)
		assert.Equal(t, []string{testAddrA}, event.Addresses)
	})

	t.Run("user_disallowed event yields the single user address", func(t *testing.T) {
		info := protocol.EventInfo{
			ID:       "disallow-1",
			TopicXDR: topicXDR(t, "user_disallowed", testAddrA),
		}
		event, err := DecodeVaultEvent(info)
		require.NoError(t, err)
		assert.Equal(t, VaultEventUserDisallowed, event.Kind)
		assert.Equal(t, []string{testAddrA}, event.Addresses)
	})

	t.Run("unrecognized event name is skipped, not an error", func(t *testing.T) {
		info := protocol.EventInfo{
			ID:       "borrowed-1",
			TopicXDR: topicXDR(t, "borrowed", testAddrA),
		}
		event, err := DecodeVaultEvent(info)
		require.NoError(t, err)
		assert.Equal(t, VaultEventUnrecognized, event.Kind)
		assert.Empty(t, event.Addresses)
	})

	t.Run("empty topics is skipped, not an error", func(t *testing.T) {
		event, err := DecodeVaultEvent(protocol.EventInfo{ID: "empty-1"})
		require.NoError(t, err)
		assert.Equal(t, VaultEventUnrecognized, event.Kind)
	})

	t.Run("fewer topics than the kind requires is an error", func(t *testing.T) {
		info := protocol.EventInfo{
			ID:       "short-deposit",
			TopicXDR: topicXDR(t, "deposit", testAddrA), // deposit needs 3 addresses
		}
		_, err := DecodeVaultEvent(info)
		assert.Error(t, err)
	})

	t.Run("malformed topic XDR is an error", func(t *testing.T) {
		info := protocol.EventInfo{
			ID:       "bad-xdr",
			TopicXDR: []string{"not-valid-base64-xdr"},
		}
		_, err := DecodeVaultEvent(info)
		assert.Error(t, err)
	})
}
