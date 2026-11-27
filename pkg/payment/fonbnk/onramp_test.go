package fonbnk

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

const testTreasury = "GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5"

func validOnRamp() StellarOnRamp {
	return StellarOnRamp{
		UserEmail: "jane@example.com", UserIP: "203.0.113.7", CountryIsoCode: "KE",
		Currency: "KES", CarrierCode: "ke_safaricom", PayerPhone: "254711111111",
		USDCAmount: 20, Destination: testTreasury, Memo: "2802699348", OrderParams: "loan-1",
	}
}

func TestStellarOnRamp_Validate(t *testing.T) {
	require.NoError(t, validOnRamp().Validate())

	cases := map[string]struct {
		mutate func(*StellarOnRamp)
		code   string
	}{
		"no email":        {func(r *StellarOnRamp) { r.UserEmail = " " }, pkgErrors.CodeMissingPayerEmail},
		"no ip":           {func(r *StellarOnRamp) { r.UserIP = "" }, pkgErrors.CodeMissingPayerIP},
		"bad ip":          {func(r *StellarOnRamp) { r.UserIP = "ussd" }, pkgErrors.CodeMissingPayerIP},
		"unspecified ip":  {func(r *StellarOnRamp) { r.UserIP = "0.0.0.0" }, pkgErrors.CodeMissingPayerIP},
		"phone with plus": {func(r *StellarOnRamp) { r.PayerPhone = "+254711111111" }, pkgErrors.CodeInvalidPhoneNumber},
		"short phone":     {func(r *StellarOnRamp) { r.PayerPhone = "0711" }, pkgErrors.CodeInvalidPhoneNumber},
		"no phone":        {func(r *StellarOnRamp) { r.PayerPhone = "" }, pkgErrors.CodeMissingPhoneNumber},
		"no carrier":      {func(r *StellarOnRamp) { r.CarrierCode = "" }, pkgErrors.CodeMissingDependency},
		"zero amount":     {func(r *StellarOnRamp) { r.USDCAmount = 0 }, pkgErrors.CodeInvalidAmount},
		"bad destination": {func(r *StellarOnRamp) { r.Destination = "0xabc" }, pkgErrors.CodeInvalidAddress},
		"text memo":       {func(r *StellarOnRamp) { r.Memo = "loan-1" }, pkgErrors.CodeInvalidMemo},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := validOnRamp()
			tc.mutate(&r)
			assert.Equal(t, tc.code, codeOf(t, r.Validate()))
		})
	}
}

func TestQuoteStellarOnRamp_Wire(t *testing.T) {
	var body map[string]any
	a, srv := newV2Adapter(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, pathQuote, r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(raw, &body))
		w.Write([]byte(`{"quoteId":"q-1"}`))
	})
	defer srv.Close()

	q, err := a.QuoteStellarOnRamp(context.Background(), validOnRamp())
	require.NoError(t, err)
	assert.Equal(t, "q-1", q.QuoteID)

	deposit, _ := body["deposit"].(map[string]any)
	payout, _ := body["payout"].(map[string]any)
	assert.Equal(t, ChannelMobileMoney, deposit["paymentChannel"])
	assert.Equal(t, TransferTypeSTKPush, deposit["transferType"])
	assert.Equal(t, "ke_safaricom", deposit["carrierCode"])
	assert.Nil(t, deposit["amount"])
	assert.Equal(t, CryptoStellarUSDC, payout["currencyCode"])
	assert.InDelta(t, 20.0, payout["amount"], 1e-9)
}

func TestCreateStellarOnRamp_Wire(t *testing.T) {
	var body map[string]any
	a, srv := newV2Adapter(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, pathOrder, r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(raw, &body))
		w.Write([]byte(`{"quoteUsed":true,"order":{"_id":"o-1","status":"deposit_awaiting"}}`))
	})
	defer srv.Close()

	quote := &Quote{
		QuoteID:        "q-1",
		QuoteExpiresAt: time.Now().Add(time.Minute),
		Deposit: QuoteLeg{
			PaymentChannel: ChannelMobileMoney, CurrencyType: CurrencyTypeFiat, CurrencyCode: "KES",
			CurrencyDetails: CurrencyDetails{CountryIsoCode: "KE", Carrier: &Carrier{Code: "ke_safaricom"}},
		},
		Payout: QuoteLeg{PaymentChannel: ChannelCrypto, CurrencyType: CurrencyTypeCrypto, CurrencyCode: CryptoStellarUSDC},
	}
	order, err := a.CreateStellarOnRamp(context.Background(), quote, validOnRamp())
	require.NoError(t, err)
	assert.Equal(t, "o-1", order.ID)

	assert.Equal(t, "q-1", body["quoteId"])
	assert.Equal(t, "jane@example.com", body["userEmail"])
	assert.Equal(t, "203.0.113.7", body["userIp"])
	assert.Equal(t, "loan-1", body["orderParams"])
	fields, _ := body["fieldsToCreateOrder"].(map[string]any)
	assert.Equal(t, testTreasury, fields[FieldBlockchainWalletAddress])
	assert.Equal(t, "2802699348", fields[FieldBlockchainMemo])
	assert.Equal(t, "254711111111", fields[FieldPhoneNumber])
	payout, _ := body["payout"].(map[string]any)
	assert.InDelta(t, 20.0, payout["amount"], 1e-9)
	deposit, _ := body["deposit"].(map[string]any)
	assert.Nil(t, deposit["amount"])
}

func TestCreateStellarOnRamp_ValidatesBeforeCalling(t *testing.T) {
	called := false
	a, srv := newV2Adapter(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.Write([]byte(`{}`))
	})
	defer srv.Close()

	r := validOnRamp()
	r.UserEmail = ""
	_, err := a.CreateStellarOnRamp(context.Background(), &Quote{QuoteID: "q-1"}, r)
	assert.Equal(t, pkgErrors.CodeMissingPayerEmail, codeOf(t, err))
	assert.False(t, called)
}

// An on-ramp failure from Fonbnk itself lands in the same domain as the
// on-ramp's own validation errors.
func TestQuoteStellarOnRamp_ErrorDomain(t *testing.T) {
	a, srv := newV2Adapter(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message":"no offers"}`))
	})
	defer srv.Close()

	_, err := a.QuoteStellarOnRamp(context.Background(), validOnRamp())
	require.Error(t, err)
	var oopsErr oops.OopsError
	require.ErrorAs(t, err, &oopsErr)
	assert.Equal(t, pkgErrors.DomainOnRamp, oopsErr.Domain())
	assert.Equal(t, OrderTypeOnRamp, oopsErr.Context()[pkgErrors.AttrDirection])

	r := validOnRamp()
	r.UserEmail = ""
	require.ErrorAs(t, r.Validate(), &oopsErr)
	assert.Equal(t, pkgErrors.DomainOnRamp, oopsErr.Domain())
}
