package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stellar/go-stellar-sdk/txnbuild"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/moneygram"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/offramp"
)

const mgTestNetworkPassphrase = "Test SDF Network ; September 2015"

// mgFakeServer mimics MoneyGram's SEP-10 + SEP-24 endpoints. Reused across
// adapter tests to keep individual test cases focused.
type mgFakeServer struct {
	t        *testing.T
	serverKP *keypair.Full
	homeDom  string

	server *httptest.Server

	// Optional hooks — set to assert request shape from inside test cases.
	onWithdraw func(t *testing.T, body map[string]string)
}

func newMGFakeServer(t *testing.T) *mgFakeServer {
	t.Helper()
	kp, err := keypair.Random()
	require.NoError(t, err)
	f := &mgFakeServer{
		t:        t,
		serverKP: kp,
		homeDom:  "stellar.moneygram.test",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/auth", f.handleAuth)
	mux.HandleFunc("/transactions/withdraw/interactive", f.handleWithdraw)
	mux.HandleFunc("/transaction", f.handleTransaction)

	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *mgFakeServer) handleAuth(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		clientAccount := r.URL.Query().Get("account")
		memoStr := r.URL.Query().Get("memo")
		require.NotEmpty(f.t, clientAccount)
		require.NotEmpty(f.t, memoStr)

		memo := txnbuild.MemoID(0)
		_, err := fmt.Sscanf(memoStr, "%d", &memo)
		require.NoError(f.t, err)

		tx, err := txnbuild.BuildChallengeTx(
			f.serverKP.Seed(),
			clientAccount,
			f.homeDom,
			f.homeDom,
			mgTestNetworkPassphrase,
			5*time.Minute,
			&memo,
		)
		require.NoError(f.t, err)

		xdrStr, err := tx.Base64()
		require.NoError(f.t, err)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"transaction":        xdrStr,
			"network_passphrase": mgTestNetworkPassphrase,
		})
	case http.MethodPost:
		// Returns a synthetic JWT with a 1h-out exp claim.
		jwt := makeMGTestJWT(time.Now().Add(time.Hour).Unix())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"token": jwt})
	}
}

func (f *mgFakeServer) handleWithdraw(w http.ResponseWriter, r *http.Request) {
	require.Equal(f.t, http.MethodPost, r.Method)
	body, _ := io.ReadAll(r.Body)
	var got map[string]string
	require.NoError(f.t, json.Unmarshal(body, &got))

	if f.onWithdraw != nil {
		f.onWithdraw(f.t, got)
	}

	_, _ = fmt.Fprintln(w, `{
		"type":"interactive_customer_info_needed",
		"url":"https://stellar.moneygram.test/sep24?token=mg-tok",
		"id":"mg-tx-001"
	}`)
}

func (f *mgFakeServer) handleTransaction(w http.ResponseWriter, _ *http.Request) {
	_, _ = fmt.Fprintln(w, `{"transaction":{
		"id":"mg-tx-001",
		"kind":"withdrawal",
		"status":"pending_user_transfer_complete",
		"amount_in":"50.00",
		"amount_out":"6420.00",
		"amount_out_asset":"iso4217:KES",
		"external_transaction_id":"REF-12345"
	}}`)
}

// makeMGTestJWT builds a minimal JWT payload with the given exp.
// Signature is empty — we only parse the exp claim downstream.
func makeMGTestJWT(exp int64) string {
	header := "eyJhbGciOiJub25lIn0" // {"alg":"none"}
	payloadBytes, _ := json.Marshal(map[string]int64{"exp": exp})
	payload := base64URL(payloadBytes)
	return header + "." + payload + "."
}

// base64URL returns RawURLEncoding for a byte slice (no padding).
func base64URL(b []byte) string {
	const alpha = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	out := make([]byte, 0, (len(b)*4+2)/3)
	for i := 0; i < len(b); i += 3 {
		var n uint32
		var p int
		for j := 0; j < 3 && i+j < len(b); j++ {
			n |= uint32(b[i+j]) << (16 - 8*j)
			p++
		}
		for k := 0; k <= p; k++ {
			out = append(out, alpha[(n>>(18-6*k))&0x3F])
		}
	}
	return string(out)
}

func newMGTestAdapter(t *testing.T, srv *mgFakeServer) *MoneyGramOffRampAdapter {
	t.Helper()
	clientKP, err := keypair.Random()
	require.NoError(t, err)

	c, err := moneygram.New(moneygram.Config{
		HomeDomain:        srv.homeDom,
		WebAuthEndpoint:   srv.server.URL + "/auth",
		TransferServerURL: srv.server.URL,
		ServerSigningKey:  srv.serverKP.Address(),
		NetworkPassphrase: mgTestNetworkPassphrase,
		USDCIssuer:        "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
		TreasurySecret:    clientKP.Seed(),
		HTTPClient:        srv.server.Client(),
		Logger:            slog.Default(),
	})
	require.NoError(t, err)

	a, err := NewMoneyGramOffRampAdapter(MoneyGramOffRampConfig{Client: c})
	require.NoError(t, err)
	return a
}

func TestMGAdapter_InitiateOffRamp_BuildsCorrectSEP9(t *testing.T) {
	srv := newMGFakeServer(t)

	srv.onWithdraw = func(t *testing.T, got map[string]string) {
		assert.Equal(t, "USDC", got["asset_code"])
		assert.Equal(t, "50.00", got["amount"])
		assert.Equal(t, "en", got["lang"])
		assert.Equal(t, "Jane", got["first_name"])
		assert.Equal(t, "Doe", got["last_name"])
		assert.Equal(t, "+254712345678", got["mobile_number"])
		assert.Equal(t, "KEN", got["address_country_code"])
		assert.Equal(t, "1990-01-15", got["birth_date"])
	}

	a := newMGTestAdapter(t, srv)

	res, err := a.InitiateOffRamp(context.Background(), offramp.Request{
		LoanID:            "L-1",
		UserID:            "U-1",
		RecipientName:     "Jane Doe",
		AmountUSD:         50.0,
		DestinationPhone:  "+254712345678",
		CountryCode:       "KE",
		BirthDate:         "1990-01-15",
		ChildAccountIndex: 7,
		PayoutMethod:      offramp.PayoutMethodCashPickup,
	})
	require.NoError(t, err)

	assert.Equal(t, "mg-tx-001", res.RequestID)
	assert.Equal(t, "L-1", res.SequenceID)
	assert.Equal(t, "cash_pickup", res.SettlementMethod)
	assert.Equal(t, "https://stellar.moneygram.test/sep24?token=mg-tok", res.InteractiveURL)
	assert.NotZero(t, res.ChildAccountMemo, "ChildAccountMemo should be derived from treasury+account_index")
	assert.Equal(t, 50.0, res.AmountUSD)
	// Cash pickup has no Stellar transfer at initiation — those fields stay empty.
	assert.Empty(t, res.StellarAddress)
	assert.Empty(t, res.StellarMemo)
	assert.Empty(t, res.StellarTxHash)
}

func TestMGAdapter_InitiateOffRamp_RejectsZeroAmount(t *testing.T) {
	srv := newMGFakeServer(t)
	a := newMGTestAdapter(t, srv)

	_, err := a.InitiateOffRamp(context.Background(), offramp.Request{
		LoanID:        "L-2",
		RecipientName: "Jane Doe",
		AmountUSD:     0,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "amount")
}

func TestMGAdapter_InitiateOffRamp_RejectsMissingRecipient(t *testing.T) {
	srv := newMGFakeServer(t)
	a := newMGTestAdapter(t, srv)

	_, err := a.InitiateOffRamp(context.Background(), offramp.Request{
		LoanID:    "L-3",
		AmountUSD: 50,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "recipient name")
}

func TestMGAdapter_InitiateOffRamp_OmitsCountryWhenISO2Unknown(t *testing.T) {
	srv := newMGFakeServer(t)

	srv.onWithdraw = func(t *testing.T, got map[string]string) {
		_, hasCountry := got["address_country_code"]
		assert.False(t, hasCountry, "unknown ISO-2 should result in omitted address_country_code, not a wrong/empty value")
	}

	a := newMGTestAdapter(t, srv)
	_, err := a.InitiateOffRamp(context.Background(), offramp.Request{
		LoanID:            "L-4",
		RecipientName:     "Jane Doe",
		AmountUSD:         25,
		CountryCode:       "ZZ", // not in the ISO map
		ChildAccountIndex: 1,
	})
	require.NoError(t, err)
}

func TestMGAdapter_StaticBehaviour(t *testing.T) {
	srv := newMGFakeServer(t)
	a := newMGTestAdapter(t, srv)

	// MoMo networks: cash pickup has none.
	nets, err := a.GetMobileMoneyNetworks(context.Background(), "KE")
	require.NoError(t, err)
	assert.Empty(t, nets)

	// Available balance: 0 — funded per-tx, not from a pre-funded balance.
	bal, err := a.GetAvailableBalance(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0.0, bal)

	// Supported providers: a single cash-pickup option.
	providers, err := a.GetSupportedProviders(context.Background(), "KE")
	require.NoError(t, err)
	require.Len(t, providers, 1)
	assert.Equal(t, "moneygram_cash_pickup", providers[0].ID)

	// FX rate without REST credentials should error explicitly.
	_, err = a.GetExchangeRate(context.Background(), "KES")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "REST credentials missing")

	// Status lookup by requestID alone is unsupported.
	_, err = a.GetOffRampStatus(context.Background(), "mg-tx-001")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
