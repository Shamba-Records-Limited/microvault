package fonbnk

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

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

func TestGetUserKYCState_Query(t *testing.T) {
	var gotQuery map[string][]string
	a, srv := newV2Adapter(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, pathUserKYC, r.URL.Path)
		gotQuery = r.URL.Query()
		w.Write([]byte(`{"requiredKycType":"basic","passedKycType":"basic","reachedKycLimit":false,
			"kycDocuments":[{"_id":"d-1","type":"basic","title":"National ID","value":"NID",
			"requiredFields":[{"key":"id_number","type":"string","label":"ID","required":true,"regexp":"^[0-9]{8}$"}]}],
			"kycSettings":[{"operationType":"deposit","currencyType":"fiat","type":"basic","min":1,"max":"Infinity"}],
			"kycUrls":{"basic":"https://pay.fonbnk.com/kyc/x"},"offrampKycRequired":false,
			"isNgBvnBlocked":false,"isNgBvnSupportLocked":false}`))
	})
	defer srv.Close()

	state, err := a.GetUserKYCState(context.Background(), KYCStateQuery{
		UserEmail: "user+tag@example.com", CountryIsoCode: "KE",
		Order: &KYCOrderAmounts{
			DepositCurrencyType: CurrencyTypeFiat, PayoutCurrencyType: CurrencyTypeCrypto,
			DepositAmountUSD: 20.5, PayoutAmountUSD: 20,
		},
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"user+tag@example.com"}, gotQuery["userEmail"], "a plus-addressed email must survive encoding")
	assert.Equal(t, []string{"20.5"}, gotQuery["depositAmountUsd"])
	assert.Equal(t, []string{"crypto"}, gotQuery["payoutCurrencyType"])

	assert.True(t, state.Satisfied())
	doc, ok := state.Document(KYCBasic)
	require.True(t, ok)
	assert.Equal(t, "^[0-9]{8}$", doc.RequiredFields[0].Regexp)
	assert.True(t, state.Settings[0].Max.Infinite)
	assert.Equal(t, "https://pay.fonbnk.com/kyc/x", state.URLs.Basic)
}

func TestKYCState_Satisfied(t *testing.T) {
	cases := []struct {
		required, passed string
		want             bool
	}{
		{"", "", true},
		{KYCBasic, "", false},
		{KYCBasic, KYCBasic, true},
		{KYCBasic, KYCAdvanced, true},
		{KYCAdvanced, KYCBasic, false},
		{KYCAdvanced, KYCAdvanced, true},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, KYCState{RequiredType: tc.required, PassedType: tc.passed}.Satisfied(),
			"required=%q passed=%q", tc.required, tc.passed)
	}
}

func TestKYCRequests_RequireEmail(t *testing.T) {
	a, srv := newV2Adapter(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("Fonbnk was called without an email")
		w.Write([]byte(`{}`))
	})
	defer srv.Close()

	_, err := a.GetUserKYCState(context.Background(), KYCStateQuery{CountryIsoCode: "KE"})
	assert.Equal(t, pkgErrors.CodeMissingPayerEmail, codeOf(t, err))
	_, err = a.SubmitUserKYC(context.Background(), KYCSubmission{DocumentID: "d-1", UserFields: map[string]any{"a": 1}})
	assert.Equal(t, pkgErrors.CodeMissingPayerEmail, codeOf(t, err))
}

func TestSubmitUserKYC_Wire(t *testing.T) {
	var body map[string]any
	a, srv := newV2Adapter(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		raw, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(raw, &body))
		w.Write([]byte(`{"currentKycStatus":"initiated","kycDocuments":[],"kycSettings":[]}`))
	})
	defer srv.Close()

	state, err := a.SubmitUserKYC(context.Background(), KYCSubmission{
		UserEmail: "jane@example.com", DocumentID: "d-1", CountryIsoCode: "KE",
		UserFields: map[string]any{"first_name": "Jane", "id_number": "12345678"},
	})
	require.NoError(t, err)
	assert.Equal(t, "initiated", state.CurrentStatus)
	assert.Equal(t, "d-1", body["documentId"])
	fields, _ := body["userFields"].(map[string]any)
	assert.Equal(t, "12345678", fields["id_number"])
}
