package stellaranchor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
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
		assert.Equal(t, "GD5NUMEX7LYHXGXCAD4PGW7JDMOUY2DKRGY5XZHJS5IONVHDKCJYGVCL", got["account"])
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
		Account:   "GD5NUMEX7LYHXGXCAD4PGW7JDMOUY2DKRGY5XZHJS5IONVHDKCJYGVCL",
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

func TestAnchorClient_InitiateDeposit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/transactions/deposit/interactive", r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "Bearer my-jwt", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, _ := io.ReadAll(r.Body)
		var got map[string]string
		require.NoError(t, json.Unmarshal(body, &got))

		assert.Equal(t, "USDC", got["asset_code"])
		assert.Equal(t, "12.40", got["amount"])
		assert.Equal(t, "sw", got["lang"])
		assert.Equal(t, "GD5NUMEX7LYHXGXCAD4PGW7JDMOUY2DKRGY5XZHJS5IONVHDKCJYGVCL", got["account"])
		assert.Equal(t, "Jane", got["first_name"])
		assert.Equal(t, "+254712345678", got["mobile_number"])

		fmt.Fprintln(w, `{
			"type":"interactive_customer_info_needed",
			"url":"https://stellar.moneygram.com/sep24?token=dep",
			"id":"dep-9931"
		}`)
	}))
	defer srv.Close()

	c, err := NewAnchorClient(AnchorConfig{TransferServerURL: srv.URL}, srv.Client(), nil)
	require.NoError(t, err)

	resp, err := c.InitiateDeposit(context.Background(), "my-jwt", DepositRequest{
		AssetCode: "USDC",
		Amount:    "12.40",
		Lang:      "sw",
		Account:   "GD5NUMEX7LYHXGXCAD4PGW7JDMOUY2DKRGY5XZHJS5IONVHDKCJYGVCL",
		Customer: Customer{
			FirstName:    "Jane",
			MobileNumber: "+254712345678",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "dep-9931", resp.ID)
	assert.Equal(t, "https://stellar.moneygram.com/sep24?token=dep", resp.URL)
	assert.Equal(t, "interactive_customer_info_needed", resp.Type)
}

func TestAnchorClient_InitiateDeposit_DefaultsAssetAndLang(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var got map[string]string
		require.NoError(t, json.Unmarshal(body, &got))
		assert.Equal(t, "USDC", got["asset_code"])
		assert.Equal(t, "en", got["lang"])

		fmt.Fprintln(w, `{"type":"interactive_customer_info_needed","url":"https://x/y","id":"id-1"}`)
	}))
	defer srv.Close()

	c, err := NewAnchorClient(AnchorConfig{TransferServerURL: srv.URL}, srv.Client(), nil)
	require.NoError(t, err)

	_, err = c.InitiateDeposit(context.Background(), "jwt", DepositRequest{
		Amount:  "15.00",
		Account: "GD5NUMEX7LYHXGXCAD4PGW7JDMOUY2DKRGY5XZHJS5IONVHDKCJYGVCL",
	})
	require.NoError(t, err)
}

func TestAnchorClient_InitiateDeposit_RejectsMissingAmount(t *testing.T) {
	c, err := NewAnchorClient(AnchorConfig{TransferServerURL: "http://example"}, nil, nil)
	require.NoError(t, err)

	_, err = c.InitiateDeposit(context.Background(), "jwt", DepositRequest{AssetCode: "USDC"})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestAnchorClient_GetTransaction_Deposit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `{"transaction":{
			"id":"dep-9931",
			"kind":"deposit",
			"status":"completed",
			"amount_in":"1650.00",
			"amount_in_asset":"iso4217:KES",
			"amount_out":"12.40",
			"amount_out_asset":"stellar:USDC:GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
			"to":"GTREASURY",
			"deposit_memo":"9182736455",
			"deposit_memo_type":"id",
			"stellar_transaction_id":"abc123def456",
			"external_transaction_id":"MG-REF-778899"
		}}`)
	}))
	defer srv.Close()

	c, err := NewAnchorClient(AnchorConfig{TransferServerURL: srv.URL}, srv.Client(), nil)
	require.NoError(t, err)

	tx, err := c.GetTransaction(context.Background(), "jwt", "dep-9931")
	require.NoError(t, err)
	assert.Equal(t, "deposit", tx.Kind)
	assert.Equal(t, StatusCompleted, tx.Status)
	assert.Equal(t, "GTREASURY", tx.To)
	assert.Equal(t, "9182736455", tx.DepositMemo)
	assert.Equal(t, "id", tx.DepositMemoType)
	assert.Equal(t, "abc123def456", tx.StellarTransactionID)
	assert.True(t, tx.Status.Terminal())
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

	// Asserted on the code, not the wording: a 404 from the anchor is a
	// distinct outcome callers branch on, and the code is what carries it.
	var oopsErr oops.OopsError
	require.ErrorAs(t, err, &oopsErr)
	assert.Equal(t, pkgErrors.CodeNotFound, oopsErr.Code())
}

func TestAnchorClient_PropagatesUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c, err := NewAnchorClient(AnchorConfig{TransferServerURL: srv.URL}, srv.Client(), nil)
	require.NoError(t, err)

	_, err = c.InitiateWithdrawal(context.Background(), "jwt", WithdrawRequest{
		Amount:  "10",
		Account: "GD5NUMEX7LYHXGXCAD4PGW7JDMOUY2DKRGY5XZHJS5IONVHDKCJYGVCL",
	})
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

func TestRefunds_ParsedFromTransaction(t *testing.T) {
	var payload struct {
		Transaction Transaction `json:"transaction"`
	}
	raw := `{"transaction":{
		"id":"mg-1","kind":"withdrawal","status":"refunded",
		"amount_in":"50.0000000","amount_in_asset":"USDC",
		"refunds":{
			"amount_refunded":"50.0000000",
			"amount_fee":"1.5000000",
			"payments":[{"id":"abc123","id_type":"stellar","amount":"50.0000000","fee":"1.5000000"}]
		}
	}}`
	require.NoError(t, json.Unmarshal([]byte(raw), &payload))

	r := payload.Transaction.Refunds
	require.NotNil(t, r)
	assert.Equal(t, "50.0000000", r.AmountRefunded)

	payments := r.StellarPayments()
	require.Len(t, payments, 1)
	assert.Equal(t, "abc123", payments[0].ID)

	net, err := r.NetRefundedStroops()
	require.NoError(t, err)
	assert.Equal(t, int64(485000000), net,
		"the anchor's stated total is 50 less a 1.5 fee; it is a cross-check, not what settles")
}

func TestRefunds_StellarPayments(t *testing.T) {
	tests := []struct {
		name    string
		refunds *Refunds
		want    int
	}{
		{"nil refunds", nil, 0},
		{"no payments", &Refunds{}, 0},
		{
			name:    "missing id_type counts as stellar",
			refunds: &Refunds{Payments: []RefundPayment{{ID: "abc", Amount: "1"}}},
			want:    1,
		},
		{
			name:    "external payments are excluded",
			refunds: &Refunds{Payments: []RefundPayment{{ID: "abc", IDType: "external"}}},
			want:    0,
		},
		{
			name:    "blank id is skipped",
			refunds: &Refunds{Payments: []RefundPayment{{IDType: "stellar"}}},
			want:    0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Len(t, tt.refunds.StellarPayments(), tt.want)
		})
	}
}

func TestRefunds_NetRefundedStroops(t *testing.T) {
	tests := []struct {
		name    string
		refunds *Refunds
		want    int64
		wantErr bool
	}{
		{"nil is zero", nil, 0, false},
		{"empty fee is zero", &Refunds{AmountRefunded: "10.0000000"}, 100000000, false},
		{
			name:    "sub-stroop precision is preserved",
			refunds: &Refunds{AmountRefunded: "49.9999999"},
			want:    499999999,
		},
		{
			name:    "full refund with fee",
			refunds: &Refunds{AmountRefunded: "50", AmountFee: "0.5"},
			want:    495000000,
		},
		{
			name:    "fee exceeding refund is rejected",
			refunds: &Refunds{AmountRefunded: "1", AmountFee: "2"},
			wantErr: true,
		},
		{
			name:    "unparseable amount is rejected",
			refunds: &Refunds{AmountRefunded: "not-a-number"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.refunds.NetRefundedStroops()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
