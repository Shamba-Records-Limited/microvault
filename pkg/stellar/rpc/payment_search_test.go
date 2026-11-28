package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/samber/oops"
	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

const (
	searchTreasury = "GCU7KVTYEPCUZOQPJ6RLNWW64FS3IBHGZTEFWERO7ZZVDQYHMG7W5B6M"
	searchIssuer   = "GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5"
	searchPayer    = "GAYF33NNNMI2Z6VNRFXQ64D4E4SF77PM46NW3ZUZEEU5X7FCHAZCMHKU"
)

// memoEnvelope is a transaction carrying memo; only the memo is read from it.
func memoEnvelope(memo string) string {
	return fmt.Sprintf(`{"tx":{"tx":{"source_account":%q,"memo":%s,"operations":[]}}}`, searchPayer, memo)
}

// searchClient serves the search's pages in order and answers a ledger
// bounded read (EndLedger set) from every event on those pages.
type searchClient struct {
	latest    protocol.GetLatestLedgerResponse
	pages     []protocol.GetEventsResponse
	all       []protocol.EventInfo
	txs       map[string]string
	requests  []protocol.GetEventsRequest
	eventsErr error
}

func (c *searchClient) GetLatestLedger(context.Context) (protocol.GetLatestLedgerResponse, error) {
	return c.latest, nil
}

func (c *searchClient) GetEvents(_ context.Context, req protocol.GetEventsRequest) (protocol.GetEventsResponse, error) {
	if req.EndLedger != 0 {
		var in []protocol.EventInfo
		for _, ev := range c.all {
			if uint32(ev.Ledger) >= req.StartLedger && uint32(ev.Ledger) < req.EndLedger {
				in = append(in, ev)
			}
		}
		return protocol.GetEventsResponse{Events: in, LatestLedger: c.latest.Sequence}, nil
	}
	c.requests = append(c.requests, req)
	if c.eventsErr != nil {
		err := c.eventsErr
		c.eventsErr = nil
		return protocol.GetEventsResponse{}, err
	}
	if len(c.pages) == 0 {
		return protocol.GetEventsResponse{LatestLedger: c.latest.Sequence}, nil
	}
	page := c.pages[0]
	c.pages = c.pages[1:]
	return page, nil
}

func (c *searchClient) GetTransaction(_ context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
	env, ok := c.txs[req.Hash]
	if !ok {
		return protocol.GetTransactionResponse{TransactionDetails: protocol.TransactionDetails{Status: protocol.TransactionStatusNotFound}}, nil
	}
	details := protocol.TransactionDetails{Status: protocol.TransactionStatusSuccess}
	for _, ev := range c.all {
		if ev.TransactionHash == req.Hash {
			details.Ledger = uint32(ev.Ledger)
		}
	}
	if req.Format == protocol.FormatJSON {
		details.EnvelopeJSON = json.RawMessage(env)
	}
	return protocol.GetTransactionResponse{TransactionDetails: details}, nil
}

// transfer is a transfer event crediting stroops; muxed renders the value as
// the map CAP-67 uses when the payment names a muxed id.
func transfer(t *testing.T, hash string, ledger int32, stroops uint64, muxed bool) protocol.EventInfo {
	t.Helper()
	amount := xdr.ScVal{Type: xdr.ScValTypeScvI128, I128: &xdr.Int128Parts{Lo: xdr.Uint64(stroops)}}
	val := amount
	if muxed {
		key, id := xdr.ScSymbol("amount"), xdr.ScSymbol("to_muxed_id")
		muxedID := xdr.Uint64(7)
		m := xdr.ScMap{
			{Key: xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &key}, Val: amount},
			{Key: xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &id}, Val: xdr.ScVal{Type: xdr.ScValTypeScvU64, U64: &muxedID}},
		}
		mp := &m
		val = xdr.ScVal{Type: xdr.ScValTypeScvMap, Map: &mp}
	}
	b64, err := xdr.MarshalBase64(val)
	require.NoError(t, err)
	return protocol.EventInfo{TransactionHash: hash, Ledger: ledger, ValueXDR: b64}
}

func search(memo string) PaymentSearch {
	return PaymentSearch{Destination: searchTreasury, AssetCode: "USDC", AssetIssuer: searchIssuer, Memo: memo, Since: time.Unix(1_000_000-600, 0)}
}

func newSearchClient() *searchClient {
	return &searchClient{latest: protocol.GetLatestLedgerResponse{Sequence: 5000, LedgerCloseTime: 1_000_000}, txs: map[string]string{}}
}

// serve puts events on one search page and behind the ledger reads.
func (c *searchClient) serve(events ...protocol.EventInfo) {
	c.pages = append(c.pages, protocol.GetEventsResponse{Events: events, LatestLedger: c.latest.Sequence})
	c.all = append(c.all, events...)
}

func newFinder(c *searchClient) *PaymentFinder {
	return NewPaymentFinder(c, "Test SDF Network ; September 2015")
}

func TestPaymentFinder_Find(t *testing.T) {
	cases := []struct {
		name   string
		memo   string
		min    int64
		events func(t *testing.T) []protocol.EventInfo
		txs    map[string]string
		ok     bool
		hash   string
		amount int64
	}{
		{
			name: "id memo matches",
			memo: "8957218622196304938",
			events: func(t *testing.T) []protocol.EventInfo {
				return []protocol.EventInfo{transfer(t, "other", 4900, 5, false), transfer(t, "ours", 4901, 192_500_010, false)}
			},
			txs: map[string]string{"other": memoEnvelope(`{"id":"1"}`), "ours": memoEnvelope(`{"id":"8957218622196304938"}`)},
			ok:  true, hash: "ours", amount: 192_500_010,
		},
		{
			name: "text memo matches",
			memo: "2802699348",
			events: func(t *testing.T) []protocol.EventInfo {
				return []protocol.EventInfo{transfer(t, "ours", 4901, 10_000_000, false)}
			},
			txs: map[string]string{"ours": memoEnvelope(`{"text":"2802699348"}`)},
			ok:  true, hash: "ours", amount: 10_000_000,
		},
		{
			name: "a value naming a muxed id is read",
			memo: "7",
			events: func(t *testing.T) []protocol.EventInfo {
				return []protocol.EventInfo{transfer(t, "ours", 4901, 42, true)}
			},
			txs: map[string]string{"ours": memoEnvelope(`{"id":"7"}`)},
			ok:  true, hash: "ours", amount: 42,
		},
		{
			name: "every credit in the transaction counts, as from a path payment",
			memo: "7",
			events: func(t *testing.T) []protocol.EventInfo {
				return []protocol.EventInfo{transfer(t, "ours", 4901, 60, false), transfer(t, "ours", 4901, 40, false)}
			},
			txs: map[string]string{"ours": memoEnvelope(`{"id":"7"}`)},
			ok:  true, hash: "ours", amount: 100,
		},
		{
			name: "earliest match wins",
			memo: "7",
			events: func(t *testing.T) []protocol.EventInfo {
				return []protocol.EventInfo{transfer(t, "first", 4900, 1, false), transfer(t, "second", 4950, 2, false)}
			},
			txs: map[string]string{"first": memoEnvelope(`{"id":"7"}`), "second": memoEnvelope(`{"id":"7"}`)},
			ok:  true, hash: "first", amount: 1,
		},
		{
			name: "dust under the memo is skipped",
			memo: "7",
			min:  100,
			events: func(t *testing.T) []protocol.EventInfo {
				return []protocol.EventInfo{transfer(t, "dust", 4900, 1, false), transfer(t, "real", 4950, 100, false)}
			},
			txs: map[string]string{"dust": memoEnvelope(`{"id":"7"}`), "real": memoEnvelope(`{"id":"7"}`)},
			ok:  true, hash: "real", amount: 100,
		},
		{
			name: "no memo is no match",
			memo: "7",
			events: func(t *testing.T) []protocol.EventInfo {
				return []protocol.EventInfo{transfer(t, "bare", 4900, 1, false)}
			},
			txs: map[string]string{"bare": memoEnvelope(`"none"`)},
		},
		{
			name: "an unknown transaction is skipped",
			memo: "7",
			events: func(t *testing.T) []protocol.EventInfo {
				return []protocol.EventInfo{transfer(t, "gone", 4900, 1, false)}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newSearchClient()
			c.serve(tc.events(t)...)
			for k, v := range tc.txs {
				c.txs[k] = v
			}
			s := search(tc.memo)
			s.MinStroops = tc.min
			found, ok, err := newFinder(c).Find(context.Background(), s)
			require.NoError(t, err)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.hash, found.TxHash)
			assert.Equal(t, tc.amount, found.AmountStroops)
		})
	}
}

// The filter must name the asset contract and the destination, and the
// search must start near Since rather than at the start of retention.
func TestPaymentFinder_Request(t *testing.T) {
	c := newSearchClient()
	_, _, err := newFinder(c).Find(context.Background(), search("7"))
	require.NoError(t, err)

	require.Len(t, c.requests, 1)
	req := c.requests[0]
	assert.Equal(t, uint32(5000-600/ledgerSeconds-searchLedgerMargin), req.StartLedger)
	require.Len(t, req.Filters, 1)
	assert.Equal(t, []string{"CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA"}, req.Filters[0].ContractIDs, "Circle testnet USDC")
	require.Len(t, req.Filters[0].Topics, 1)
	assert.Len(t, req.Filters[0].Topics[0], 4)
	_, err = json.Marshal(req)
	assert.NoError(t, err, "the filter is valid")
}

// Each response covers a bounded ledger window: a short page whose cursor is
// behind the latest ledger is followed, not taken as the end.
func TestPaymentFinder_FollowsTheCursor(t *testing.T) {
	c := newSearchClient()
	c.pages = []protocol.GetEventsResponse{{Cursor: protocol.Cursor{Ledger: 4900}.String(), LatestLedger: 5000}}
	c.serve(transfer(t, "ours", 4990, 3, false))
	c.txs["ours"] = memoEnvelope(`{"id":"7"}`)

	found, ok, err := newFinder(c).Find(context.Background(), search("7"))
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "ours", found.TxHash)
	require.Len(t, c.requests, 2)
	assert.Zero(t, c.requests[1].StartLedger, "a cursor request carries no start ledger")
	require.NotNil(t, c.requests[1].Pagination.Cursor)
}

func TestPaymentFinder_WithoutCloseTimeSearchesAllRetention(t *testing.T) {
	c := newSearchClient()
	c.latest.LedgerCloseTime = 0
	_, _, err := newFinder(c).Find(context.Background(), search("7"))
	require.NoError(t, err)
	assert.Equal(t, uint32(1), c.requests[0].StartLedger)
}

func TestPaymentFinder_ClampsToRetention(t *testing.T) {
	c := newSearchClient()
	c.eventsErr = errors.New("startLedger must be within the ledger range: 4980 - 5000")
	c.serve(transfer(t, "ours", 4990, 3, false))
	c.txs["ours"] = memoEnvelope(`{"id":"7"}`)

	_, ok, err := newFinder(c).Find(context.Background(), search("7"))
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, uint32(4980), c.requests[1].StartLedger)
}

func TestPaymentFinder_Validation(t *testing.T) {
	f := newFinder(newSearchClient())
	_, _, err := f.Find(context.Background(), PaymentSearch{Destination: searchTreasury, AssetCode: "USDC", AssetIssuer: searchIssuer})
	assert.Equal(t, pkgErrors.CodeInvalidMemo, codeOf(t, err))
	_, _, err = f.Find(context.Background(), PaymentSearch{Memo: "7"})
	assert.Equal(t, pkgErrors.CodeMissingAccount, codeOf(t, err))
}

// A hash the payer reports is measured by the transfers it credited, so a
// path payment is counted where reading payment operations would see none.
func TestPaymentFinder_AmountIn(t *testing.T) {
	c := newSearchClient()
	c.serve(transfer(t, "reported", 4901, 60, false), transfer(t, "reported", 4901, 40, true), transfer(t, "other", 4901, 999, false))
	c.txs["reported"] = memoEnvelope(`"none"`)
	f := newFinder(c)

	amount, err := f.AmountIn(context.Background(), "reported", search(""))
	require.NoError(t, err)
	assert.Equal(t, int64(100), amount)

	_, err = f.AmountIn(context.Background(), "missing", search(""))
	assert.Equal(t, pkgErrors.CodeNotFound, codeOf(t, err))
	_, err = f.AmountIn(context.Background(), "", search(""))
	assert.Equal(t, pkgErrors.CodeMissingAccount, codeOf(t, err))
}

func TestTransferAmount(t *testing.T) {
	plain := transfer(t, "x", 1, 9_999_670, false)
	got, ok := transferAmount(plain.ValueXDR)
	assert.True(t, ok)
	assert.Equal(t, int64(9_999_670), got)

	_, ok = transferAmount("not xdr")
	assert.False(t, ok)
	big, err := xdr.MarshalBase64(xdr.ScVal{Type: xdr.ScValTypeScvI128, I128: &xdr.Int128Parts{Hi: 1}})
	require.NoError(t, err)
	_, ok = transferAmount(big)
	assert.False(t, ok, "an amount beyond int64 is refused")
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	oe, ok := oops.AsOops(err)
	require.True(t, ok, "not an oops error: %v", err)
	return fmt.Sprint(oe.Code())
}
