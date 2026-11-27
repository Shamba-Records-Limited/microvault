package chipper

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/govalues/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

const testTreasury = "GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5"

func repaymentOrder() OrderRequest {
	return OrderRequest{
		From:              OrderLeg{Code: "ke_mpesa", AccountNumber: "254711111111", Currency: "KES"},
		To:                OrderLeg{Code: MethodUSDCStellar, AccountNumber: testTreasury, Tag: "2802699348", Currency: "USDC", Amount: NewAmount(decimal.MustParse("19.250001"))},
		ExternalReference: "MV-LN-1-rp1",
	}
}

const orderJSON = `{"order":{"id":"ord_1","status":"awaiting_funds","externalReference":"MV-LN-1-rp1",
	"from":{"code":"ke_mpesa","accountNumber":"254711111111","amount":"2540.00","currency":"KES"},
	"to":{"code":"usdc_stellar","accountNumber":"` + testTreasury + `","tag":"2802699348","amount":"19.250001","currency":"USDC"},
	"instructions":{"type":"mobile_money","message":"Approve the prompt","amount":"2578.10","currency":"KES"},
	"expectedAmount":"2578.10","fee":{"amount":"38.10","currency":"KES"},"rate":"131.95",
	"createdAt":"2026-11-27T10:00:00Z","updatedAt":"2026-11-27T10:00:00Z"}}`

func TestCreateOrder(t *testing.T) {
	var body map[string]any
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/orders", r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(raw, &body))
		status := http.StatusCreated
		if calls > 1 {
			status = http.StatusOK
		}
		writeJSON(t, w, status, orderJSON)
	})

	o, replayed, err := c.CreateOrder(context.Background(), repaymentOrder())
	require.NoError(t, err)
	assert.False(t, replayed)
	assert.Equal(t, "ord_1", o.ID)
	assert.Equal(t, "2578.10", o.Instructions.Amount.String())
	assert.False(t, o.IsTerminal())

	to, _ := body["to"].(map[string]any)
	assert.Equal(t, testTreasury, to["accountNumber"])
	assert.Equal(t, "2802699348", to["tag"])
	assert.InDelta(t, 19.250001, to["amount"], 1e-9)
	from, _ := body["from"].(map[string]any)
	assert.Nil(t, from["amount"])

	_, replayed, err = c.CreateOrder(context.Background(), repaymentOrder())
	require.NoError(t, err)
	assert.True(t, replayed)
}

func TestCreateOrder_Validation(t *testing.T) {
	cases := map[string]struct {
		mutate func(*OrderRequest)
		code   string
	}{
		"no reference":      {func(r *OrderRequest) { r.ExternalReference = "" }, pkgErrors.CodeInvalidConfig},
		"no destination":    {func(r *OrderRequest) { r.To.AccountNumber = "" }, pkgErrors.CodeMissingAccount},
		"no amount":         {func(r *OrderRequest) { r.To.Amount = nil }, pkgErrors.CodeMissingAmount},
		"both amounts":      {func(r *OrderRequest) { r.From.Amount = NewAmount(decimal.MustParse("10")) }, pkgErrors.CodeInvalidAmount},
		"too many decimals": {func(r *OrderRequest) { r.To.Amount = NewAmount(decimal.MustParse("1.0000001")) }, pkgErrors.CodeInvalidAmount},
		"long tag":          {func(r *OrderRequest) { r.To.Tag = string(make([]byte, 101)) }, pkgErrors.CodeInvalidMemo},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := newTestClient(t, func(http.ResponseWriter, *http.Request) {
				t.Error("Chipper was called with an invalid order")
			})
			req := repaymentOrder()
			tc.mutate(&req)
			_, _, err := c.CreateOrder(context.Background(), req)
			assert.Equal(t, tc.code, codeOf(t, err))
		})
	}
}

func TestGetOrder(t *testing.T) {
	var paths []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		writeJSON(t, w, http.StatusOK, orderJSON)
	})

	_, err := c.GetOrder(context.Background(), "ord_1")
	require.NoError(t, err)
	_, err = c.GetOrderByReference(context.Background(), "MV-LN-1-rp1")
	require.NoError(t, err)
	assert.Equal(t, []string{"/v1/orders/ord_1", "/v1/orders/by-reference/MV-LN-1-rp1"}, paths)

	_, err = c.GetOrder(context.Background(), "")
	assert.Equal(t, pkgErrors.CodeMissingAccount, codeOf(t, err))
}

func TestEventOrder(t *testing.T) {
	ev := Event{Event: EventOrderCompleted, Data: json.RawMessage(`{"id":"ord_1","status":"completed","payoutId":"pay_1","externalReference":"MV-LN-1-rp1"}`)}
	o, err := ev.Order()
	require.NoError(t, err)
	assert.True(t, o.IsTerminal())
	require.NotNil(t, o.PayoutID)
	assert.Equal(t, "pay_1", *o.PayoutID)
}

func TestPayoutTxHash(t *testing.T) {
	cases := map[string]struct {
		payout string
		hash   string
		ok     bool
	}{
		"details.hash":            {`{"details":{"hash":"abc"}}`, "abc", true},
		"details.transactionHash": {`{"details":{"transactionHash":"def"}}`, "def", true},
		"top level":               {`{"transactionHash":"ghi"}`, "ghi", true},
		"absent":                  {`{}`, "", false},
		"details not an object":   {`{"details":"x"}`, "", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var p Payout
			require.NoError(t, json.Unmarshal([]byte(tc.payout), &p))
			hash, ok := p.TxHash()
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.hash, hash)
		})
	}
}
