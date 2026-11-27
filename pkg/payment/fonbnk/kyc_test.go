package fonbnk

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

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

// The email travels in the query string; a transport failure must not carry
// the URL into the error.
func TestGetUserKYCState_TransportErrorOmitsEmail(t *testing.T) {
	a := NewFonbnkAdapter("cid", "c2VjcmV0", "http://127.0.0.1:1")
	_, err := a.GetUserKYCState(context.Background(), KYCStateQuery{UserEmail: "jane@example.com", CountryIsoCode: "KE"})
	require.Error(t, err)
	assert.Equal(t, pkgErrors.CodeTransportFailed, codeOf(t, err))
	assert.NotContains(t, err.Error(), "jane")
	assert.NotContains(t, err.Error(), "userEmail")
}

func TestParseError_BoundsTheMessage(t *testing.T) {
	a, srv := newV2Adapter(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(strings.Repeat("x", 4*maxErrorMessage)))
	})
	defer srv.Close()

	_, err := a.GetUserKYCState(context.Background(), KYCStateQuery{UserEmail: "jane@example.com", CountryIsoCode: "KE"})
	require.Error(t, err)
	msg, _ := contextOf(t, err)["api_message"].(string)
	assert.Len(t, msg, maxErrorMessage)
}
