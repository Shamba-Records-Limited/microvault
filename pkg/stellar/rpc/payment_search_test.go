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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

const (
	searchTreasury = "GCU7KVTYEPCUZOQPJ6RLNWW64FS3IBHGZTEFWERO7ZZVDQYHMG7W5B6M"
	searchIssuer   = "GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5"
	searchPayer    = "GAYF33NNNMI2Z6VNRFXQ64D4E4SF77PM46NW3ZUZEEU5X7FCHAZCMHKU"
)

func usdcEnvelope(memo, dest, amount string) string {
	return fmt.Sprintf(`{"tx":{"tx":{"source_account":%q,"memo":%s,"operations":[{"body":{"payment":{"destination":%q,
		"asset":{"credit_alphanum4":{"asset_code":"USDC","issuer":%q}},"amount":%q}}}]}}}`, searchPayer, memo, dest, searchIssuer, amount)
}

type searchClient struct {
	latest    protocol.GetLatestLedgerResponse
	pages     []protocol.GetEventsResponse
	txs       map[string]string
	requests  []protocol.GetEventsRequest
	eventsErr error
}

func (c *searchClient) GetLatestLedger(context.Context) (protocol.GetLatestLedgerResponse, error) {
	return c.latest, nil
}

func (c *searchClient) GetEvents(_ context.Context, req protocol.GetEventsRequest) (protocol.GetEventsResponse, error) {
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
	return protocol.GetTransactionResponse{TransactionDetails: protocol.TransactionDetails{
		Status: protocol.TransactionStatusSuccess, EnvelopeJSON: json.RawMessage(env),
	}}, nil
}

func event(hash string, ledger int32) protocol.EventInfo {
	return protocol.EventInfo{TransactionHash: hash, Ledger: ledger}
}

func search(memo string) PaymentSearch {
	return PaymentSearch{Destination: searchTreasury, AssetCode: "USDC", AssetIssuer: searchIssuer, Memo: memo, Since: time.Unix(1_000_000-600, 0)}
}

func newSearchClient() *searchClient {
	return &searchClient{latest: protocol.GetLatestLedgerResponse{Sequence: 5000, LedgerCloseTime: 1_000_000}, txs: map[string]string{}}
}

func TestPaymentFinder_Find(t *testing.T) {
	cases := []struct {
		name   string
		memo   string
		min    int64
		events []protocol.EventInfo
		txs    map[string]string
		ok     bool
		hash   string
		amount int64
	}{
		{
			name:   "id memo matches",
			memo:   "8957218622196304938",
			events: []protocol.EventInfo{event("other", 4900), event("ours", 4901)},
			txs: map[string]string{
				"other": usdcEnvelope(`{"id":"1"}`, searchTreasury, "5"),
				"ours":  usdcEnvelope(`{"id":"8957218622196304938"}`, searchTreasury, "192500010"),
			},
			ok: true, hash: "ours", amount: 192_500_010,
		},
		{
			name:   "text memo matches",
			memo:   "2802699348",
			events: []protocol.EventInfo{event("ours", 4901)},
			txs:    map[string]string{"ours": usdcEnvelope(`{"text":"2802699348"}`, searchTreasury, "10000000")},
			ok:     true, hash: "ours", amount: 10_000_000,
		},
		{
			name:   "earliest match wins",
			memo:   "7",
			events: []protocol.EventInfo{event("first", 4900), event("second", 4950)},
			txs: map[string]string{
				"first":  usdcEnvelope(`{"id":"7"}`, searchTreasury, "1"),
				"second": usdcEnvelope(`{"id":"7"}`, searchTreasury, "2"),
			},
			ok: true, hash: "first", amount: 1,
		},
		{
			name:   "dust under the memo is skipped",
			memo:   "7",
			min:    100,
			events: []protocol.EventInfo{event("dust", 4900), event("real", 4950)},
			txs: map[string]string{
				"dust": usdcEnvelope(`{"id":"7"}`, searchTreasury, "1"),
				"real": usdcEnvelope(`{"id":"7"}`, searchTreasury, "100"),
			},
			ok: true, hash: "real", amount: 100,
		},
		{
			name:   "no memo is no match",
			memo:   "7",
			events: []protocol.EventInfo{event("bare", 4900)},
			txs:    map[string]string{"bare": usdcEnvelope(`"none"`, searchTreasury, "1")},
		},
		{
			name:   "a payment elsewhere is no match",
			memo:   "7",
			events: []protocol.EventInfo{event("away", 4900)},
			txs:    map[string]string{"away": usdcEnvelope(`{"id":"7"}`, searchPayer, "1")},
		},
		{
			name:   "an unknown transaction is skipped",
			memo:   "7",
			events: []protocol.EventInfo{event("gone", 4900)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newSearchClient()
			c.pages = []protocol.GetEventsResponse{{Events: tc.events, LatestLedger: 5000}}
			for k, v := range tc.txs {
				c.txs[k] = v
			}
			s := search(tc.memo)
			s.MinStroops = tc.min
			found, ok, err := NewPaymentFinder(c, "Test SDF Network ; September 2015").Find(context.Background(), s)
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
	_, _, err := NewPaymentFinder(c, "Test SDF Network ; September 2015").Find(context.Background(), search("7"))
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
	c.pages = []protocol.GetEventsResponse{
		{Cursor: protocol.Cursor{Ledger: 4900}.String(), LatestLedger: 5000},
		{Events: []protocol.EventInfo{event("ours", 4990)}, LatestLedger: 5000},
	}
	c.txs["ours"] = usdcEnvelope(`{"id":"7"}`, searchTreasury, "3")

	found, ok, err := NewPaymentFinder(c, "Test SDF Network ; September 2015").Find(context.Background(), search("7"))
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
	_, _, err := NewPaymentFinder(c, "Test SDF Network ; September 2015").Find(context.Background(), search("7"))
	require.NoError(t, err)
	assert.Equal(t, uint32(1), c.requests[0].StartLedger)
}

func TestPaymentFinder_ClampsToRetention(t *testing.T) {
	c := newSearchClient()
	c.eventsErr = errors.New("startLedger must be within the ledger range: 4980 - 5000")
	c.pages = []protocol.GetEventsResponse{{Events: []protocol.EventInfo{event("ours", 4990)}, LatestLedger: 5000}}
	c.txs["ours"] = usdcEnvelope(`{"id":"7"}`, searchTreasury, "3")

	_, ok, err := NewPaymentFinder(c, "Test SDF Network ; September 2015").Find(context.Background(), search("7"))
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, uint32(4980), c.requests[1].StartLedger)
}

func TestPaymentFinder_Validation(t *testing.T) {
	f := NewPaymentFinder(newSearchClient(), "Test SDF Network ; September 2015")
	_, _, err := f.Find(context.Background(), PaymentSearch{Destination: searchTreasury, AssetCode: "USDC", AssetIssuer: searchIssuer})
	assert.Equal(t, pkgErrors.CodeInvalidMemo, codeOf(t, err))
	_, _, err = f.Find(context.Background(), PaymentSearch{Memo: "7"})
	assert.Equal(t, pkgErrors.CodeMissingAccount, codeOf(t, err))
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	oe, ok := oops.AsOops(err)
	require.True(t, ok, "not an oops error: %v", err)
	return fmt.Sprint(oe.Code())
}
