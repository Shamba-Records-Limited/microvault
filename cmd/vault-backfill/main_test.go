package main

import (
	"context"
	"testing"

	"github.com/stellar/go-stellar-sdk/keypair"
	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	accountID := xdr.MustAddress(addr)
	scAddr, err := xdr.NewScAddress(xdr.ScAddressTypeScAddressTypeAccount, accountID)
	require.NoError(t, err)
	val := xdr.ScVal{Type: xdr.ScValTypeScvAddress, Address: &scAddr}
	out, err := xdr.MarshalBase64(val)
	require.NoError(t, err)
	return out
}

type fakeEventsClient struct {
	latest protocol.GetLatestLedgerResponse
	pages  []protocol.GetEventsResponse
	calls  []protocol.GetEventsRequest
}

func (f *fakeEventsClient) GetLatestLedger(context.Context) (protocol.GetLatestLedgerResponse, error) {
	return f.latest, nil
}

func (f *fakeEventsClient) GetEvents(_ context.Context, req protocol.GetEventsRequest) (protocol.GetEventsResponse, error) {
	f.calls = append(f.calls, req)
	i := len(f.calls) - 1
	if i >= len(f.pages) {
		return protocol.GetEventsResponse{}, nil
	}
	return f.pages[i], nil
}

func TestScanDepositors_ProbesBeforeScanning(t *testing.T) {
	addrA := keypair.MustRandom().Address()
	client := &fakeEventsClient{
		latest: protocol.GetLatestLedgerResponse{Sequence: 1000},
		pages: []protocol.GetEventsResponse{
			{OldestLedger: 500, LatestLedger: 1000}, // probe
			{
				OldestLedger: 500,
				LatestLedger: 1000,
				Events: []protocol.EventInfo{
					{ID: "e1", TopicXDR: topicXDR(t, "user_allowed", addrA)},
				},
			},
		},
	}

	addresses, oldest, latest, err := scanDepositors(context.Background(), client, "CCONTRACT")

	require.NoError(t, err)
	assert.Equal(t, []string{addrA}, addresses)
	assert.Equal(t, uint32(500), oldest)
	assert.Equal(t, uint32(1000), latest)

	require.Len(t, client.calls, 2)
	assert.Equal(t, uint32(1000), client.calls[0].StartLedger, "the probe uses the latest ledger")
	assert.Equal(t, uint32(500), client.calls[1].StartLedger, "the real scan starts from OldestLedger")
}

func TestScanDepositors_FollowsTheCursorAcrossPages(t *testing.T) {
	addrA := keypair.MustRandom().Address()
	addrB := keypair.MustRandom().Address()

	fullPage := make([]protocol.EventInfo, eventsPageLimit)
	for i := range fullPage {
		fullPage[i] = protocol.EventInfo{ID: "pad", TopicXDR: topicXDR(t, "user_allowed", addrA)}
	}

	client := &fakeEventsClient{
		latest: protocol.GetLatestLedgerResponse{Sequence: 1000},
		pages: []protocol.GetEventsResponse{
			{OldestLedger: 500, LatestLedger: 1000}, // probe
			{OldestLedger: 500, LatestLedger: 700, Events: fullPage, Cursor: protocol.Cursor{Ledger: 700}.String()},
			{
				OldestLedger: 500,
				LatestLedger: 1000,
				Events: []protocol.EventInfo{
					{ID: "e2", TopicXDR: topicXDR(t, "user_allowed", addrB)},
				},
			},
		},
	}

	addresses, _, latest, err := scanDepositors(context.Background(), client, "CCONTRACT")

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{addrA, addrB}, addresses)
	assert.Equal(t, uint32(1000), latest)
	require.Len(t, client.calls, 3)
	assert.Zero(t, client.calls[2].StartLedger, "cursor and StartLedger cannot both be set")
	require.NotNil(t, client.calls[2].Pagination.Cursor)
}

func TestScanDepositors_SkipsUndecodableEventsWithoutFailing(t *testing.T) {
	client := &fakeEventsClient{
		latest: protocol.GetLatestLedgerResponse{Sequence: 1000},
		pages: []protocol.GetEventsResponse{
			{OldestLedger: 500, LatestLedger: 1000}, // probe
			{
				OldestLedger: 500,
				LatestLedger: 1000,
				Events: []protocol.EventInfo{
					{ID: "bad", TopicXDR: []string{"not-valid-base64-xdr"}},
				},
			},
		},
	}

	addresses, _, _, err := scanDepositors(context.Background(), client, "CCONTRACT")

	require.NoError(t, err)
	assert.Empty(t, addresses)
}
