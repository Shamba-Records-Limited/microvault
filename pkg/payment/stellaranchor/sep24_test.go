package stellaranchor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnchorClient_InitiateWithdrawal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/transactions/withdraw/interactive", r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "Bearer my-jwt", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, _ := io.ReadAll(r.Body)
		var got map[string]string
		require.NoError(t, json.Unmarshal(body, &got))

		assert.Equal(t, "USDC", got["asset_code"])
		assert.Equal(t, "50.00", got["amount"])
		assert.Equal(t, "en", got["lang"])
		assert.Equal(t, "Jane", got["first_name"])
		assert.Equal(t, "Doe", got["last_name"])
		assert.Equal(t, "+254712345678", got["mobile_number"])
		assert.Equal(t, "KEN", got["address_country_code"])
		// SEP-9 fields we didn't set must be absent (not "" — actually omitted)
		_, hasBirth := got["birth_date"]
		assert.False(t, hasBirth)

		fmt.Fprintln(w, `{
			"type":"interactive_customer_info_needed",
			"url":"https://stellar.moneygram.com/sep24?token=xyz",
			"id":"82fhs729f63dh0v4"
		}`)
	}))
	defer srv.Close()

	c, err := NewAnchorClient(AnchorConfig{TransferServerURL: srv.URL}, srv.Client(), nil)
	require.NoError(t, err)

	resp, err := c.InitiateWithdrawal(context.Background(), "my-jwt", WithdrawRequest{
		AssetCode: "USDC",
		Amount:    "50.00",
		Customer: Customer{
			FirstName:          "Jane",
			LastName:           "Doe",
			MobileNumber:       "+254712345678",
			AddressCountryCode: "KEN",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "82fhs729f63dh0v4", resp.ID)
	assert.Equal(t, "https://stellar.moneygram.com/sep24?token=xyz", resp.URL)
	assert.Equal(t, "interactive_customer_info_needed", resp.Type)
}

func TestAnchorClient_GetTransaction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/transaction", r.URL.Path)
		assert.Equal(t, "tx-123", r.URL.Query().Get("id"))
		assert.Equal(t, "Bearer jwt", r.Header.Get("Authorization"))

		fmt.Fprintln(w, `{"transaction":{
			"id":"tx-123",
			"kind":"withdrawal",
			"status":"pending_user_transfer_complete",
			"amount_in":"50.00",
			"amount_in_asset":"stellar:USDC:GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
			"amount_out":"6420.00",
			"amount_out_asset":"iso4217:KES",
			"amount_fee":"0.50",
			"withdraw_anchor_account":"GANCH",
			"withdraw_memo":"4242",
			"withdraw_memo_type":"id",
			"external_transaction_id":"REF-12345678",
			"more_info_url":"https://stellar.moneygram.com/info/tx-123"
		}}`)
	}))
	defer srv.Close()

	c, err := NewAnchorClient(AnchorConfig{TransferServerURL: srv.URL}, srv.Client(), nil)
	require.NoError(t, err)

	tx, err := c.GetTransaction(context.Background(), "jwt", "tx-123")
	require.NoError(t, err)
	assert.Equal(t, "tx-123", tx.ID)
	assert.Equal(t, StatusPendingUserTransferComplete, tx.Status)
	assert.Equal(t, "50.00", tx.AmountIn)
	assert.Equal(t, "6420.00", tx.AmountOut)
	assert.Equal(t, "iso4217:KES", tx.AmountOutAsset)
	assert.Equal(t, "REF-12345678", tx.ExternalTransactionID)
	assert.Equal(t, "GANCH", tx.WithdrawAnchorAccount)
	assert.Equal(t, "4242", tx.WithdrawMemo)
	assert.False(t, tx.Status.Terminal())
}

func TestAnchorClient_GetTransaction_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c, err := NewAnchorClient(AnchorConfig{TransferServerURL: srv.URL}, srv.Client(), nil)
	require.NoError(t, err)

	_, err = c.GetTransaction(context.Background(), "jwt", "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAnchorClient_PropagatesUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c, err := NewAnchorClient(AnchorConfig{TransferServerURL: srv.URL}, srv.Client(), nil)
	require.NoError(t, err)

	_, err = c.InitiateWithdrawal(context.Background(), "jwt", WithdrawRequest{Amount: "10"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnauthorized)

	_, err = c.GetTransaction(context.Background(), "jwt", "tx")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnauthorized)
}

func TestAnchorClient_RejectsMissingAmount(t *testing.T) {
	c, err := NewAnchorClient(AnchorConfig{TransferServerURL: "http://example"}, nil, nil)
	require.NoError(t, err)

	_, err = c.InitiateWithdrawal(context.Background(), "jwt", WithdrawRequest{AssetCode: "USDC"})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestStatusTerminal(t *testing.T) {
	terminal := []Status{
		StatusCompleted, StatusRefunded, StatusExpired,
		StatusNoMarket, StatusTooSmall, StatusTooLarge, StatusError,
	}
	for _, s := range terminal {
		assert.True(t, s.Terminal(), "%s should be terminal", s)
	}

	nonTerminal := []Status{
		StatusIncomplete, StatusPendingAnchor, StatusPendingExternal,
		StatusPendingUserTransferStart, StatusPendingUserTransferComplete,
	}
	for _, s := range nonTerminal {
		assert.False(t, s.Terminal(), "%s should NOT be terminal", s)
	}
}
