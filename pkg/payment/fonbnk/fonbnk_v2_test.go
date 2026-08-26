package fonbnk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var oopsErr oops.OopsError
	require.True(t, errors.As(err, &oopsErr), "error is not an oops error: %v", err)
	code, _ := oopsErr.Code().(string)
	return code
}

func contextOf(t *testing.T, err error) map[string]any {
	t.Helper()
	var oopsErr oops.OopsError
	require.True(t, errors.As(err, &oopsErr))
	return oopsErr.Context()
}

// newV2Adapter points an adapter at a test server. clientSecret must be valid
// base64 or the signing transport rejects every request.
func newV2Adapter(handler http.HandlerFunc) (*FonbnkAdapter, *httptest.Server) {
	srv := httptest.NewServer(handler)
	return NewFonbnkAdapter("cid", "c2VjcmV0", srv.URL), srv
}

func ptr(v float64) *float64 { return &v }

// Fonbnk sends "Infinity" for the top band of every fee table, so a plain
// float64 would fail to decode any complete fee schedule.
func TestMaxBound_DecodesNumberAndInfinity(t *testing.T) {
	var settings []FeeSetting
	raw := `[
		{"id":"provider_fee","recipient":"provider","type":"flat_amount","value":30,"min":1001,"max":2500},
		{"id":"service_fee","recipient":"platform","type":"percentage","value":0,"min":0,"max":"Infinity"}
	]`
	require.NoError(t, json.Unmarshal([]byte(raw), &settings))
	require.Len(t, settings, 2)

	assert.False(t, settings[0].Max.Infinite)
	assert.Equal(t, 2500.0, settings[0].Max.Value)
	assert.True(t, settings[1].Max.Infinite)

	assert.True(t, settings[0].Max.Covers(2500))
	assert.False(t, settings[0].Max.Covers(2501))
	assert.True(t, settings[1].Max.Covers(1e12))
}

func TestMaxBound_RoundTrips(t *testing.T) {
	for _, in := range []string{`2500`, `"Infinity"`} {
		var m MaxBound
		require.NoError(t, json.Unmarshal([]byte(in), &m))
		out, err := json.Marshal(m)
		require.NoError(t, err)
		assert.JSONEq(t, in, string(out))
	}
}

// deposit_canceled and deposit_expired read as endings but Fonbnk still
// accepts a late payment and runs the payout.
func TestIsTerminal(t *testing.T) {
	final := []string{StatusPayoutSuccessful, StatusRefundSuccessful}
	notFinal := []string{
		StatusDepositAwaiting, StatusDepositValidating, StatusDepositSuccessful,
		StatusDepositInvalid, StatusDepositCanceled, StatusDepositExpired,
		StatusPayoutPending, StatusPayoutFailed,
		StatusRefundInitiated, StatusRefundPending, StatusRefundFailed,
	}
	for _, s := range final {
		assert.True(t, IsTerminal(s), s)
	}
	for _, s := range notFinal {
		assert.False(t, IsTerminal(s), s)
	}
}

func TestExactlyOneAmount(t *testing.T) {
	errb := fonbnkErr("test")

	require.NoError(t, exactlyOneAmount(errb, ptr(10), nil))
	require.NoError(t, exactlyOneAmount(errb, nil, ptr(10)))

	err := exactlyOneAmount(errb, nil, nil)
	require.Error(t, err)
	assert.Equal(t, pkgErrors.CodeMissingAmount, codeOf(t, err))

	err = exactlyOneAmount(errb, ptr(1), ptr(2))
	require.Error(t, err)
	assert.Equal(t, pkgErrors.CodeInvalidAmount, codeOf(t, err))
}

func TestCreateQuote_RejectsTwoAmountsWithoutCalling(t *testing.T) {
	var called bool
	a, srv := newV2Adapter(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.Write([]byte(`{}`))
	})
	defer srv.Close()

	_, err := a.CreateQuote(context.Background(), QuoteRequest{
		Deposit: QuoteDepositLeg{LegSpec: LegSpec{Amount: ptr(20)}},
		Payout:  LegSpec{Amount: ptr(2498)},
	})
	require.Error(t, err)
	assert.False(t, called, "the request must not reach Fonbnk")
}

// The signed endpoint is path plus query, so the query must reach the wire
// exactly as rendered.
func TestGetOrder_QueryReachesTheWireUnchanged(t *testing.T) {
	var gotPath, gotQuery string
	a, srv := newV2Adapter(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		w.Write([]byte(`{"_id":"o1","status":"deposit_awaiting"}`))
	})
	defer srv.Close()

	got, err := a.GetOrder(context.Background(), "o1")
	require.NoError(t, err)
	assert.Equal(t, "o1", got.ID)
	assert.Equal(t, pathOrder, gotPath)
	assert.Equal(t, "orderId=o1", gotQuery)
}

func TestGetOrderByParams(t *testing.T) {
	var gotQuery string
	a, srv := newV2Adapter(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"_id":"o1"}`))
	})
	defer srv.Close()

	_, err := a.GetOrderByParams(context.Background(), "loan-42")
	require.NoError(t, err)
	assert.Equal(t, "orderParams=loan-42", gotQuery)
}

// Quoting is ungated and order creation is not, so this 403 is what a working
// integration hits first. It needs to be recognisable.
func TestCreateOrder_PermissionGate(t *testing.T) {
	a, srv := newV2Adapter(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"This feature is not available for this merchant, please contact support"}`))
	})
	defer srv.Close()

	_, err := a.CreateOrder(context.Background(), CreateOrderRequest{
		UserEmail: "u@example.com",
		Deposit:   LegSpec{Amount: ptr(20)},
	})
	require.Error(t, err)
	assert.Equal(t, pkgErrors.CodeMerchantNotPermitted, codeOf(t, err))
}

func TestCall_MapsStatusCodes(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"unauthorized", http.StatusUnauthorized, `{"message":"Signature is invalid"}`, pkgErrors.CodeUnauthorized},
		{"not found", http.StatusNotFound, `{"code":"ORDER_NOT_AVAILABLE"}`, pkgErrors.CodeNotFound},
		{"other", http.StatusBadRequest, `{"message":"The order is expired"}`, pkgErrors.CodeHTTPError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, srv := newV2Adapter(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			})
			defer srv.Close()

			_, err := a.GetOrder(context.Background(), "o1")
			require.Error(t, err)
			assert.Equal(t, tt.want, codeOf(t, err))

			ctx := contextOf(t, err)
			assert.Equal(t, "o1", ctx[pkgErrors.AttrOrderID], "the failing order must be named")
			assert.Equal(t, tt.status, ctx[pkgErrors.AttrStatusCode])
		})
	}
}

// The observed sandbox off-ramp: 20 USDC in, 2468 KES out after a 30 KES fee.
// exchangeRateAfterFees reports 126.4281 for the same quote and rises as the
// payout falls, so it must not be the number compared across providers.
func TestQuote_EffectiveRate(t *testing.T) {
	offRamp := Quote{
		Deposit: QuoteLeg{CurrencyType: CurrencyTypeCrypto, Cashout: Cashout{AmountBeforeFees: 20, AmountAfterFees: 20}},
		Payout: QuoteLeg{CurrencyType: CurrencyTypeFiat, Cashout: Cashout{
			AmountBeforeFees: 2498, AmountAfterFees: 2468,
			Rate: 124.9098, RateAfterFees: 126.4281,
		}},
	}
	assert.Equal(t, OrderTypeOffRamp, offRamp.Direction())

	sell, ok := offRamp.EffectiveRate()
	require.True(t, ok)
	assert.InDelta(t, 123.4, sell, 0.0001)
	assert.Less(t, sell, offRamp.Payout.Cashout.RateAfterFees,
		"exchangeRateAfterFees flatters the payout")

	// The observed sandbox on-ramp: 2653 KES buys 20 USDC.
	onRamp := Quote{
		Deposit: QuoteLeg{CurrencyType: CurrencyTypeFiat, Cashout: Cashout{AmountBeforeFees: 2653, AmountAfterFees: 2653}},
		Payout:  QuoteLeg{CurrencyType: CurrencyTypeCrypto, Cashout: Cashout{AmountBeforeFees: 20.01, AmountAfterFees: 20}},
	}
	assert.Equal(t, OrderTypeOnRamp, onRamp.Direction())

	buy, ok := onRamp.EffectiveRate()
	require.True(t, ok)
	assert.InDelta(t, 132.65, buy, 0.0001)

	// Both sides are the same unit, so the round trip is a subtraction.
	assert.Greater(t, buy, sell, "buying costs more than selling yields")
	assert.InDelta(t, 0.0697, (buy-sell)/buy, 0.0001)
}

func TestQuote_EffectiveRate_UnknownDirection(t *testing.T) {
	_, ok := Quote{}.EffectiveRate()
	assert.False(t, ok)

	// A merchant-balance corridor has no fiat-per-crypto rate.
	q := Quote{
		Deposit: QuoteLeg{CurrencyType: CurrencyTypeCrypto, Cashout: Cashout{AmountBeforeFees: 100}},
		Payout:  QuoteLeg{CurrencyType: CurrencyTypeMerchantBalance, Cashout: Cashout{AmountAfterFees: 100}},
	}
	assert.Empty(t, q.Direction())
	_, ok = q.EffectiveRate()
	assert.False(t, ok)
}

func TestQuote_RequiredFieldKeys(t *testing.T) {
	q := Quote{
		Deposit: QuoteLeg{FieldsToCreateOrder: []RequiredField{
			{Key: FieldBlockchainWalletAddress, Required: false},
			{Key: FieldPhoneNumber, Required: true},
		}},
		Payout: QuoteLeg{FieldsToCreateOrder: []RequiredField{
			{Key: FieldBlockchainMemo, Required: false},
			{Key: "bankCode", Required: true},
		}},
	}
	assert.Equal(t, []string{FieldPhoneNumber, "bankCode"}, q.RequiredFieldKeys())
}

// Calling the endpoint at the attempt cap expires the order, so the gate has
// to hold before any request is made.
func TestCanRetryIntermediateAction(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	later := now.Add(time.Minute)

	tests := []struct {
		name string
		in   TransferInstructions
		want bool
	}{
		{"available", TransferInstructions{IsIntermediateActionAvailable: true, IntermediateActionMaxAttempts: 3, IntermediateActionAttempts: 1}, true},
		{"not available", TransferInstructions{IsIntermediateActionAvailable: false, IntermediateActionMaxAttempts: 3}, false},
		{"at cap", TransferInstructions{IsIntermediateActionAvailable: true, IntermediateActionMaxAttempts: 3, IntermediateActionAttempts: 3}, false},
		{"past cap", TransferInstructions{IsIntermediateActionAvailable: true, IntermediateActionMaxAttempts: 3, IntermediateActionAttempts: 4}, false},
		{"too early", TransferInstructions{IsIntermediateActionAvailable: true, IntermediateActionMaxAttempts: 3, IntermediateActionNextAttemptAvailableAt: &later}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.in.CanRetryIntermediateAction(now))
		})
	}
}

func TestCreateOrderFromQuote_RefusesExpiredQuote(t *testing.T) {
	var called bool
	a, srv := newV2Adapter(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.Write([]byte(`{}`))
	})
	defer srv.Close()

	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return now }

	quote := &Quote{QuoteID: "q1", QuoteExpiresAt: now.Add(-time.Second)}
	_, err := a.CreateOrderFromQuote(context.Background(), quote, CreateOrderRequest{})

	require.Error(t, err)
	assert.Equal(t, pkgErrors.CodeQuoteExpired, codeOf(t, err))
	assert.Equal(t, "q1", contextOf(t, err)[pkgErrors.AttrQuoteID])
	assert.False(t, called, "an expired quote must not reach Fonbnk")
}

// The legs must come from the quote, not from the caller, or the order can be
// created at a price that was never quoted.
func TestCreateOrderFromQuote_CopiesLegsFromQuote(t *testing.T) {
	var body CreateOrderRequest
	a, srv := newV2Adapter(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Write([]byte(`{"quoteUsed":true,"order":{"_id":"o1"}}`))
	})
	defer srv.Close()

	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return now }

	quote := &Quote{
		QuoteID:        "q1",
		QuoteExpiresAt: now.Add(time.Minute),
		Deposit: QuoteLeg{
			PaymentChannel: ChannelCrypto, CurrencyType: CurrencyTypeCrypto, CurrencyCode: "STELLAR_USDC",
		},
		Payout: QuoteLeg{
			PaymentChannel: ChannelMobileMoney, CurrencyType: CurrencyTypeFiat, CurrencyCode: "KES",
			CurrencyDetails: CurrencyDetails{
				CountryIsoCode: "KE",
				Carrier:        &Carrier{Code: "ke_safaricom", Name: "Safaricom Kenya"},
			},
		},
	}

	got, err := a.CreateOrderFromQuote(context.Background(), quote, CreateOrderRequest{
		UserEmail: "u@example.com",
		Payout:    LegSpec{Amount: ptr(2498)},
	})
	require.NoError(t, err)
	assert.True(t, got.QuoteUsed)

	assert.Equal(t, "q1", body.QuoteID)
	assert.Equal(t, "STELLAR_USDC", body.Deposit.CurrencyCode)
	assert.Equal(t, ChannelCrypto, body.Deposit.PaymentChannel)
	assert.Nil(t, body.Deposit.Amount)
	assert.Equal(t, "KES", body.Payout.CurrencyCode)
	assert.Equal(t, "KE", body.Payout.CountryIsoCode)
	assert.Equal(t, "ke_safaricom", body.Payout.CarrierCode)
	require.NotNil(t, body.Payout.Amount)
	assert.Equal(t, 2498.0, *body.Payout.Amount)
}

// Fonbnk rejects unknown keys, and transferType is only legal on a quote's
// deposit leg.
func TestQuoteOnRamp_SendsTransferTypeOnDepositOnly(t *testing.T) {
	var body map[string]any
	a, srv := newV2Adapter(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Write([]byte(`{"quoteId":"q1"}`))
	})
	defer srv.Close()

	fiat := FiatLeg{CurrencyCode: "KES", CountryIsoCode: "KE", CarrierCode: "ke_safaricom", TransferType: TransferTypeOTPSTKPush}
	_, err := a.QuoteOnRamp(context.Background(), fiat, CryptoLeg{CurrencyCode: "STELLAR_USDC"}, 20)
	require.NoError(t, err)

	deposit := body["deposit"].(map[string]any)
	payout := body["payout"].(map[string]any)
	assert.Equal(t, TransferTypeOTPSTKPush, deposit["transferType"])
	assert.NotContains(t, payout, "transferType", "transferType is legal on the quote deposit leg only")
	// The amount rides the crypto leg in both directions, which on an on-ramp
	// is the payout.
	assert.NotContains(t, deposit, "amount", "only one leg carries an amount")
	assert.Equal(t, 20.0, payout["amount"])
}

func TestQuoteOffRamp_AmountOnTheCryptoLeg(t *testing.T) {
	var body map[string]any
	a, srv := newV2Adapter(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Write([]byte(`{"quoteId":"q1"}`))
	})
	defer srv.Close()

	_, err := a.QuoteOffRamp(context.Background(),
		CryptoLeg{CurrencyCode: "STELLAR_USDC"},
		FiatLeg{CurrencyCode: "KES", CountryIsoCode: "KE", CarrierCode: "ke_safaricom"},
		20)
	require.NoError(t, err)

	deposit := body["deposit"].(map[string]any)
	payout := body["payout"].(map[string]any)
	assert.NotContains(t, payout, "amount", "only one leg carries an amount")
	assert.Equal(t, 20.0, deposit["amount"], "the crypto leg carries it on an off-ramp")
	assert.Equal(t, ChannelCrypto, deposit["paymentChannel"])
}

// A dead corridor arrives as 200 with every field zero, which would otherwise
// be shown to a user as a limit of zero.
func TestGetOrderLimits_AllZeroIsNotTradable(t *testing.T) {
	a, srv := newV2Adapter(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"deposit":{"min":0,"max":0,"minUsd":0,"maxUsd":0,"step":0,"supportsDecimals":false},
			"payout":{"min":0,"max":0,"minUsd":0,"maxUsd":0,"step":0,"supportsDecimals":false}}`))
	})
	defer srv.Close()

	_, err := a.GetOrderLimits(context.Background(), OrderLimitsQuery{DepositCurrencyCode: "KES"})
	require.Error(t, err)
	assert.Equal(t, pkgErrors.CodeCorridorUnavailable, codeOf(t, err))
}

func TestGetOrderLimits_Tradable(t *testing.T) {
	var gotQuery string
	a, srv := newV2Adapter(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"deposit":{"min":1494,"max":747367,"minUsd":1,"maxUsd":500,"step":1,"supportsDecimals":false},
			"payout":{"min":1,"max":500,"minUsd":1,"maxUsd":500,"step":0.000001,"supportsDecimals":true}}`))
	})
	defer srv.Close()

	limits, err := a.GetOrderLimits(context.Background(), OrderLimitsQuery{
		DepositPaymentChannel: ChannelMobileMoney,
		DepositCurrencyType:   CurrencyTypeFiat,
		DepositCurrencyCode:   "KES",
		DepositCountryIsoCode: "KE",
		PayoutPaymentChannel:  ChannelCrypto,
		PayoutCurrencyType:    CurrencyTypeCrypto,
		PayoutCurrencyCode:    "STELLAR_USDC",
	})
	require.NoError(t, err)
	assert.Equal(t, 500.0, limits.Payout.Max)
	assert.True(t, limits.Payout.SupportsDecimals)
	assert.False(t, limits.Deposit.SupportsDecimals)
	assert.NotContains(t, gotQuery, "CarrierCode=", "unset params must be omitted")
	assert.NotContains(t, gotQuery, "payoutCountryIsoCode")
}

func TestConfirmOrder_SendsFields(t *testing.T) {
	var body map[string]any
	a, srv := newV2Adapter(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Write([]byte(`{"_id":"o1","status":"deposit_validating"}`))
	})
	defer srv.Close()

	got, err := a.ConfirmOrder(context.Background(), "o1", map[string]string{FieldBlockchainTxHash: "abc123"})
	require.NoError(t, err)
	assert.Equal(t, StatusDepositValidating, got.Status)
	assert.Equal(t, "o1", body["orderId"])

	fields := body["fieldsToConfirmOrder"].(map[string]any)
	assert.Equal(t, "abc123", fields[FieldBlockchainTxHash])
}

func TestSubmitOTPAndRetrySTKPush(t *testing.T) {
	var body map[string]any
	a, srv := newV2Adapter(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Write([]byte(`{"_id":"o1"}`))
	})
	defer srv.Close()

	_, err := a.SubmitOTP(context.Background(), "o1", "123456")
	require.NoError(t, err)
	fields := body["fieldsForIntermediateAction"].(map[string]any)
	assert.Equal(t, "123456", fields[FieldOTPCode])

	body = nil
	_, err = a.RetrySTKPush(context.Background(), "o1")
	require.NoError(t, err)
	assert.NotContains(t, body, "fieldsForIntermediateAction", "a plain retry sends no fields")
}

func TestGetAvailableCurrencies(t *testing.T) {
	a, srv := newV2Adapter(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`[
			{"currencyType":"fiat","currencyCode":"KES","currencyDetails":{"countryIsoCode":"KE"},
			 "pairs":["crypto"],
			 "paymentChannels":[
			   {"name":"M-PESA","type":"mobile_money","transferTypes":["stk_push"],"isDepositAllowed":true,"isPayoutAllowed":true,
			    "carriers":[{"code":"ke_safaricom","name":"Safaricom Kenya"}]},
			   {"name":"Paybill","type":"paybill","transferTypes":[],"isDepositAllowed":false,"isPayoutAllowed":true}]},
			{"currencyType":"crypto","currencyCode":"STELLAR_USDC",
			 "currencyDetails":{"network":"STELLAR","asset":"USDC","contractAddress":"GA5Z"},
			 "pairs":["fiat"],
			 "paymentChannels":[{"name":"Crypto","type":"crypto","transferTypes":["manual"],"isDepositAllowed":true,"isPayoutAllowed":true}]}
		]`))
	})
	defer srv.Close()

	currencies, err := a.GetAvailableCurrencies(context.Background())
	require.NoError(t, err)
	require.Len(t, currencies, 2)

	kes, ok := FindCurrency(currencies, "KES", "KE")
	require.True(t, ok)
	assert.Len(t, kes.DepositChannels(), 1)
	assert.Len(t, kes.PayoutChannels(), 2)

	momo, ok := kes.Channel(ChannelMobileMoney)
	require.True(t, ok)
	require.Len(t, momo.Carriers, 1)
	assert.Equal(t, "ke_safaricom", momo.Carriers[0].Code)

	_, ok = FindCurrency(currencies, "KES", "NG")
	assert.False(t, ok, "country narrows a currency that spans several")
}

func TestWebhook_SignatureRoundTrip(t *testing.T) {
	body := []byte(`{"event":"order-status-change","data":{"order":{"_id":"o1"}}}`)
	const secret = "whsec"

	require.NoError(t, VerifyWebhookSignature(body, webhookSignature(body, secret), secret))
}

// Hashing secret-then-body, or re-serialising the JSON, both produce a
// signature that never matches.
func TestWebhook_SignatureRejects(t *testing.T) {
	body := []byte(`{"event":"order-status-change","data":{}}`)
	const secret = "whsec"
	good := webhookSignature(body, secret)

	tests := map[string]struct {
		body      []byte
		signature string
		secret    string
		wantCode  string
	}{
		"tampered body":   {[]byte(`{"event":"order-status-change","data":{"x":1}}`), good, secret, pkgErrors.CodeUnauthorized},
		"wrong secret":    {body, good, "other", pkgErrors.CodeUnauthorized},
		"reserialised":    {[]byte(`{"data":{},"event":"order-status-change"}`), good, secret, pkgErrors.CodeUnauthorized},
		"no signature":    {body, "", secret, pkgErrors.CodeUnauthorized},
		"no secret wired": {body, good, "", pkgErrors.CodeMissingDependency},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := VerifyWebhookSignature(tt.body, tt.signature, tt.secret)
			require.Error(t, err)
			assert.Equal(t, tt.wantCode, codeOf(t, err))
		})
	}
}

func TestParseWebhook(t *testing.T) {
	body := []byte(`{"event":"order-status-change","data":{"order":{
		"_id":"o1","merchantOrderParams":"loan-42","status":"payout_successful","type":"on_ramp",
		"payout":{"paymentChannel":"crypto","currencyCode":"STELLAR_USDC",
			"transaction":{"meta":{"transactionHash":"deadbeef"}}}},
		"userKyc":{"latestKycStatus":"approved"}}}`)
	const secret = "whsec"

	event, err := ParseWebhook(body, webhookSignature(body, secret), secret)
	require.NoError(t, err)

	assert.Equal(t, EventOrderStatusChange, event.Event)
	assert.Equal(t, "loan-42", event.Data.Order.MerchantOrderParams)
	assert.Equal(t, StatusPayoutSuccessful, event.Data.Order.Status)
	require.NotNil(t, event.Data.Order.Payout.Transaction)
	assert.Equal(t, "deadbeef", event.Data.Order.Payout.Transaction.Meta.TransactionHash)
	assert.Equal(t, "approved", event.Data.UserKyc.LatestKycStatus)
}

// A leg with no on-chain movement carries no transaction block at all.
func TestParseWebhook_NoTransactionOnFiatLeg(t *testing.T) {
	body := []byte(`{"event":"order-status-change","data":{"order":{"_id":"o1",
		"deposit":{"paymentChannel":"mobile_money","currencyCode":"KES"}}}}`)
	const secret = "whsec"

	event, err := ParseWebhook(body, webhookSignature(body, secret), secret)
	require.NoError(t, err)
	assert.Nil(t, event.Data.Order.Deposit.Transaction)
}

func TestOrder_DecodesFullSandboxPayload(t *testing.T) {
	a, srv := newV2Adapter(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{
			"_id":"o1","countryIsoCode":"KE","userEmail":"u@example.com","status":"payout_successful",
			"deposit":{"paymentChannel":"mobile_money","currencyType":"fiat","currencyCode":"KES",
				"currencyDetails":{"countryIsoCode":"KE","carrier":{"code":"ke_safaricom","name":"Safaricom Kenya","_id":"x"}},
				"cashout":{"amountBeforeFees":135,"amountAfterFees":130,"exchangeRate":130.08,"exchangeRateAfterFees":135.0831,
					"feeSettings":[{"id":"service_fee","recipient":"platform","type":"percentage","value":2.5,"min":0,"max":"Infinity"}],
					"chargedFees":[{"id":"service_fee","type":"percentage","recipient":"platform","amount":3.38}],
					"chargedFeesPerRecipient":{"platform":3.38}},
				"transferInstructions":{"type":"otp_stk_push","otpChannel":"sms",
					"intermediateActionAttempts":2,"intermediateActionMaxAttempts":3,
					"isIntermediateActionAvailable":true,
					"intermediateActionNextAttemptAvailableAt":"2025-11-27T12:00:14.390Z",
					"instructionsText":"x","transferDetails":[{"id":"amountToSend","label":"Amount to send","value":"135"}],
					"fieldsToConfirmOrder":[],
					"fieldsForIntermediateAction":[{"key":"otpCode","label":"OTP code","type":"number","required":true}]}},
			"payout":{"paymentChannel":"crypto","currencyType":"crypto","currencyCode":"POLYGON_USDT",
				"currencyDetails":{"network":"POLYGON","asset":"USDT","contractAddress":"0x3b3a"},
				"cashout":{"amountBeforeFees":1.000645,"amountAfterFees":1},
				"transaction":{"meta":{"transactionHash":"0xe168","toAddress":"0x5b7a"}}},
			"statusChangeLogs":[{"newStatus":"deposit_canceled","date":"2025-11-27T12:01:43.660Z"},
				{"oldStatus":"payout_pending","newStatus":"payout_successful","date":"2025-11-27T12:02:19.053Z"}],
			"createdAt":"2025-11-27T11:59:03.754Z","updatedAt":"2025-11-27T12:02:19.124Z","expiresAt":"2025-11-27T12:04:03.673Z"}`))
	})
	defer srv.Close()

	order, err := a.GetOrder(context.Background(), "o1")
	require.NoError(t, err)

	assert.Equal(t, StatusPayoutSuccessful, order.Status)
	require.NotNil(t, order.Deposit.CurrencyDetails.Carrier)
	assert.Equal(t, "ke_safaricom", order.Deposit.CurrencyDetails.Carrier.Code)

	require.NotNil(t, order.Deposit.TransferInstructions)
	ti := order.Deposit.TransferInstructions
	assert.Equal(t, TransferTypeOTPSTKPush, ti.Type)
	assert.Equal(t, "sms", ti.OtpChannel)
	require.Len(t, ti.FieldsForIntermediateAction, 1)
	require.Len(t, order.Deposit.Cashout.FeeSettings, 1)
	assert.True(t, order.Deposit.Cashout.FeeSettings[0].Max.Infinite)

	assert.Nil(t, order.Payout.TransferInstructions, "only the deposit leg carries instructions")
	require.NotNil(t, order.Payout.Transaction)
	assert.Equal(t, "0xe168", order.Payout.Transaction.Meta.TransactionHash)
	assert.Empty(t, order.Payout.Transaction.Meta.FromAddress)

	require.Len(t, order.StatusChangeLogs, 2)
	assert.Empty(t, order.StatusChangeLogs[0].OldStatus, "a deposit_canceled entry has no oldStatus")
	assert.Equal(t, StatusPayoutPending, order.StatusChangeLogs[1].OldStatus)
}
