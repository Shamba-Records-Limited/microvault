package chipper

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/govalues/decimal"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

const testKey = "sk_test_abc123"

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var oopsErr oops.OopsError
	require.True(t, errors.As(err, &oopsErr), "error is not an oops error: %v", err)
	code, _ := oopsErr.Code().(string)
	return code
}

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(Config{APIKey: testKey, BaseURL: srv.URL, HTTPClient: srv.Client()})
	require.NoError(t, err)
	return c
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, err := io.WriteString(w, body)
	require.NoError(t, err)
}

const payoutJSON = `{
  "id": "pay_kes_abc", "status": "pending",
  "from": {"accountNumber": "USDC12345678", "amount": "7.678911", "currency": "USDC"},
  "to": {"accountNumber": "254712345678", "code": "ke_mpesa", "amount": "1000.00", "currency": "KES"},
  "fee": {"amount": "0.115184", "currency": "USDC"},
  "rate": "130.226805", "narration": null, "externalReference": "loan-1",
  "metadata": {}, "statusMessage": null, "completedAt": null,
  "createdAt": "2026-11-27T10:00:00Z", "updatedAt": "2026-11-27T10:00:00Z"
}`

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"test key on sandbox", Config{APIKey: testKey}, false},
		{"live key on production", Config{APIKey: "sk_live_x", BaseURL: ProductionBaseURL}, false},
		{"missing key", Config{}, true},
		{"unknown prefix", Config{APIKey: "pk_test_x"}, true},
		{"live key on sandbox", Config{APIKey: "sk_live_x"}, true},
		{"test key on production", Config{APIKey: testKey, BaseURL: ProductionBaseURL + "/"}, true},
		{"plain http remote", Config{APIKey: testKey, BaseURL: "http://sandbox-api.platform.chipper.ai"}, true},
		{"no host", Config{APIKey: testKey, BaseURL: "https://"}, true},
		{"http loopback", Config{APIKey: testKey, BaseURL: "http://127.0.0.1:8080"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := New(tt.cfg)
			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, pkgErrors.CodeInvalidConfig, codeOf(t, err))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.cfg.APIKey == "sk_live_x", c.IsLive())
		})
	}
}

func TestCreatePayout(t *testing.T) {
	var gotBody map[string]any
	status := http.StatusCreated
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/payouts", r.URL.Path)
		assert.Equal(t, "Bearer "+testKey, r.Header.Get("Authorization"))
		assert.Equal(t, APIVersion, r.Header.Get("chipper-version"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		writeJSON(t, w, status, `{"payout": `+payoutJSON+`}`)
	})

	req := PayoutRequest{
		From: &PayoutSource{Amount: NewAmount(decimal.MustParse("7.678911")), Currency: "USDC"},
		To: PayoutDestination{
			Code: "ke_mpesa", AccountNumber: "254712345678", Currency: "KES",
			Recipient: &Recipient{Name: "Jane Doe"},
		},
		ExternalReference: "loan-1",
	}

	p, replayed, err := c.CreatePayout(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, replayed)
	assert.Equal(t, "pay_kes_abc", p.ID)
	assert.Equal(t, "0.115184", p.Fee.Amount.String())
	assert.Equal(t, "130.226805", p.Rate.String())
	assert.False(t, p.IsTerminal())

	from := gotBody["from"].(map[string]any)
	assert.InDelta(t, 7.678911, from["amount"], 1e-9, "amount must be sent as a JSON number")
	assert.NotContains(t, gotBody["to"], "amount")

	status = http.StatusOK
	_, replayed, err = c.CreatePayout(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, replayed)
}

func TestCreatePayoutValidation(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no request expected")
	})
	base := func() PayoutRequest {
		return PayoutRequest{
			To:                PayoutDestination{Code: "ke_mpesa", AccountNumber: "254712345678", Currency: "KES", Amount: NewAmount(decimal.MustParse("1000"))},
			ExternalReference: "loan-1",
		}
	}
	long := make([]byte, 101)
	for i := range long {
		long[i] = 'a'
	}

	tests := []struct {
		name   string
		mutate func(*PayoutRequest)
		code   string
	}{
		{"no reference", func(r *PayoutRequest) { r.ExternalReference = "" }, pkgErrors.CodeInvalidConfig},
		{"reference too long", func(r *PayoutRequest) { r.ExternalReference = string(long) }, pkgErrors.CodeInvalidConfig},
		{"no destination", func(r *PayoutRequest) { r.To.AccountNumber = "" }, pkgErrors.CodeMissingAccount},
		{"no amount", func(r *PayoutRequest) { r.To.Amount = nil }, pkgErrors.CodeMissingAmount},
		{"both amounts", func(r *PayoutRequest) {
			r.From = &PayoutSource{Amount: NewAmount(decimal.MustParse("1")), Currency: "USDC"}
		}, pkgErrors.CodeInvalidAmount},
		{"too many kes decimals", func(r *PayoutRequest) { r.To.Amount = NewAmount(decimal.MustParse("10.001")) }, pkgErrors.CodeInvalidAmount},
		{"too many usdc decimals", func(r *PayoutRequest) {
			r.To.Amount = nil
			r.From = &PayoutSource{Amount: NewAmount(decimal.MustParse("1.1234567")), Currency: "USDC"}
		}, pkgErrors.CodeInvalidAmount},
		{"zero amount", func(r *PayoutRequest) { r.To.Amount = NewAmount(decimal.Zero) }, pkgErrors.CodeInvalidAmount},
		{"from amount without currency", func(r *PayoutRequest) {
			r.To.Amount = nil
			r.From = &PayoutSource{Amount: NewAmount(decimal.MustParse("1"))}
		}, pkgErrors.CodeMissingAccount},
		{"kyc without account name", func(r *PayoutRequest) { r.To.KYC = map[string]any{"idType": "NID"} }, pkgErrors.CodeMissingAccount},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := base()
			tt.mutate(&req)
			_, _, err := c.CreatePayout(context.Background(), req)
			require.Error(t, err)
			assert.Equal(t, tt.code, codeOf(t, err))
		})
	}

	req := base()
	req.To.Amount = NewAmount(decimal.MustParse("10.50"))
	assert.NoError(t, validatePayout(req), "trailing zeros within scale are fine")
}

func TestErrorParsing(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		header     map[string]string
		wantCode   string
		wantAPI    string
		retryable  bool
		retryAfter time.Duration
	}{
		{
			name: "insufficient funds", status: 400,
			body:     `{"error":"insufficient_funds","message":"Insufficient funds","details":{"available":"1.00"},"requestId":"req_1"}`,
			wantCode: pkgErrors.CodeInsufficientLiquidity, wantAPI: CodeInsufficientFunds,
		},
		{
			name: "rate limited with details", status: 429,
			body:     `{"error":"rate_limited","message":"slow down","details":{"retryAfter":2},"requestId":"req_2"}`,
			wantCode: pkgErrors.CodeRateLimited, wantAPI: CodeRateLimited, retryable: true, retryAfter: 2 * time.Second,
		},
		{
			name: "rate limited with header", status: 429, body: `{"error":"rate_limited","message":"x"}`,
			header:   map[string]string{"Retry-After": "3"},
			wantCode: pkgErrors.CodeRateLimited, wantAPI: CodeRateLimited, retryable: true, retryAfter: 3 * time.Second,
		},
		{
			name: "forbidden", status: 403, body: `{"error":"forbidden","message":"payouts not enabled"}`,
			wantCode: pkgErrors.CodePermissionDenied, wantAPI: CodeForbidden,
		},
		{
			name: "no provider", status: 503, body: `{"error":"no_provider_available","message":"x"}`,
			wantCode: pkgErrors.CodeCorridorUnavailable, wantAPI: CodeNoProviderAvailable, retryable: true,
		},
		{
			name: "non-json gateway error", status: 502, body: `<html>bad gateway</html>`,
			wantCode: pkgErrors.CodeHTTPError, retryable: true,
		},
		{
			name: "bare 401", status: 401, body: ``,
			wantCode: pkgErrors.CodeUnauthorized,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("x-request-id", "req_hdr")
				for k, v := range tt.header {
					w.Header().Set(k, v)
				}
				writeJSON(t, w, tt.status, tt.body)
			})
			_, err := c.GetOrganization(context.Background())
			require.Error(t, err)
			assert.Equal(t, tt.wantCode, codeOf(t, err))

			apiErr, ok := AsAPIError(err)
			require.True(t, ok)
			assert.Equal(t, tt.status, apiErr.StatusCode)
			assert.Equal(t, tt.wantAPI, apiErr.Code)
			assert.Equal(t, tt.retryable, apiErr.Retryable())
			assert.Equal(t, tt.retryAfter, apiErr.RetryAfter)
			assert.NotEmpty(t, apiErr.RequestID)
		})
	}
}

func TestGetRate(t *testing.T) {
	body := `{"rate":{"from":"USDC","to":"KES","rate":"130.226805"}}`
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/rates/USDC/KES", r.URL.Path)
		writeJSON(t, w, 200, body)
	})

	rate, err := c.GetRate(context.Background(), "usdc", "kes")
	require.NoError(t, err)
	assert.Equal(t, "130.226805", rate.Rate.String())

	body = `{"rate":{"from":"USDC","to":"KES","rate":"0"}}`
	_, err = c.GetRate(context.Background(), "USDC", "KES")
	require.Error(t, err)
	assert.Equal(t, pkgErrors.CodeRateUnavailable, codeOf(t, err))
}

func TestListAccountsPaginates(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "100", r.URL.Query().Get("limit"))
		if r.URL.Query().Get("cursor") == "" {
			writeJSON(t, w, 200, `{"data":[{"id":"KES1","currency":"KES","availableBalance":"10.00","pendingBalance":"0","totalBalance":"10.00","estimatedUsd":null,"createdAt":"2026-11-27T10:00:00Z"}],"hasMore":true,"nextCursor":"c2"}`)
			return
		}
		assert.Equal(t, "c2", r.URL.Query().Get("cursor"))
		writeJSON(t, w, 200, `{"data":[{"id":"USDC1","currency":"USDC","availableBalance":"1477.500000","pendingBalance":"0","totalBalance":"1477.500000","estimatedUsd":"1477.50","createdAt":"2026-11-27T10:00:00Z"}],"hasMore":false,"nextCursor":null}`)
	})

	accounts, err := c.ListAccounts(context.Background())
	require.NoError(t, err)
	require.Len(t, accounts, 2)

	usdc, ok, err := c.AccountFor(context.Background(), "usdc")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "1477.5", usdc.AvailableBalance.Trim(0).String())

	_, ok, err = c.AccountFor(context.Background(), "GHS")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestListLedgerQuery(t *testing.T) {
	from := time.Date(2026, 11, 27, 0, 0, 0, 0, time.FixedZone("EAT", 3*3600))
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		assert.Equal(t, "/v1/ledger", r.URL.Path)
		assert.Equal(t, LedgerFeeDebit, q.Get("type"))
		assert.Equal(t, "pay_kes_abc", q.Get("referenceId"))
		assert.Equal(t, "2026-11-26T21:00:00Z", q.Get("from"))
		assert.False(t, q.Has("to"))
		writeJSON(t, w, 200, `{"data":[{"id":"le_1","sequence":42,"type":"fee_debit","amount":"-0.115184","currency":"USDC","accountNumber":"USDC12345678","balanceAfter":"1469.705906","referenceType":"payout","referenceId":"pay_kes_abc","externalReference":"loan-1","createdAt":"2026-11-27T10:00:00Z"}],"hasMore":false,"nextCursor":null}`)
	})

	page, err := c.ListLedger(context.Background(), LedgerFilter{Type: LedgerFeeDebit, ReferenceID: "pay_kes_abc", From: from})
	require.NoError(t, err)
	require.Len(t, page.Data, 1)
	assert.Equal(t, int64(42), page.Data[0].Sequence)
	assert.Equal(t, "-0.115184", page.Data[0].Amount.String())
}

func TestCreateCryptoAddress(t *testing.T) {
	body := `{"address":{"id":"addr_1","currency":"USDC","chain":"Stellar","address":"GABC","tag":"123456","externalReference":null,"createdAt":"2026-11-27T10:00:00Z"}}`
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req CryptoAddressRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, ChainStellar, req.Chain)
		writeJSON(t, w, 201, body)
	})

	addr, err := c.CreateCryptoAddress(context.Background(), CryptoAddressRequest{Currency: "USDC", Chain: ChainStellar})
	require.NoError(t, err)
	assert.Equal(t, "GABC", *addr.Address)
	assert.Equal(t, "123456", *addr.Tag)

	body = `{"address":{"id":"addr_1","currency":"USDC","chain":"Stellar","address":null,"tag":null,"externalReference":null,"createdAt":"2026-11-27T10:00:00Z"}}`
	_, err = c.CreateCryptoAddress(context.Background(), CryptoAddressRequest{Currency: "USDC", Chain: ChainStellar})
	require.Error(t, err)
	assert.Equal(t, pkgErrors.CodeIncompleteResponse, codeOf(t, err))
}

func TestCapabilitiesLookups(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/capabilities", r.URL.Path)
		writeJSON(t, w, 200, `{"capabilities":{
		  "payouts":[{"country":{"code":"KE","name":"Kenya","flag":""},"currency":{"code":"KES","name":"Kenyan Shilling"},
		    "methods":[{"code":"ke_mpesa","name":"M-PESA","type":"mobile_money","status":"degraded","limits":{"min":10,"max":150000,"currency":"KES"},"estimatedSettlement":"instant","fields":[]}]}],
		  "collections":[],
		  "deposits":[{"currency":"USDC","name":"USD Coin","chains":[{"chain":"Stellar","network":"testnet","code":"usdc_stellar","requiresMemo":true}]}]}}`)
	})

	caps, err := c.GetCapabilities(context.Background())
	require.NoError(t, err)

	m, ok := caps.PayoutMethod("ke", "ke_mpesa")
	require.True(t, ok)
	assert.True(t, m.IsUsable())
	assert.Equal(t, "150000", m.Limits.Max.String())

	_, ok = caps.PayoutMethod("GH", "ke_mpesa")
	assert.False(t, ok)

	ch, ok := caps.DepositChain("usdc", "stellar")
	require.True(t, ok)
	assert.True(t, ch.RequiresMemo)
}

func sign(t *testing.T, key []byte, id, ts string, body []byte) string {
	t.Helper()
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "."))
	mac.Write(body)
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestWebhookVerifier(t *testing.T) {
	key := []byte("super-secret-signing-key-bytes!!")
	secret := webhookSecretPrefix + base64.StdEncoding.EncodeToString(key)
	now := time.Unix(1_790_000_000, 0)

	v, err := NewWebhookVerifier(secret)
	require.NoError(t, err)
	v.now = func() time.Time { return now }

	body := []byte(`{"event":"payout.completed","data":` + payoutJSON + `}`)
	id := "whd_1"
	ts := strconv.FormatInt(now.Unix(), 10)
	good := sign(t, key, id, ts, body)

	event, err := v.Parse(id, ts, good, body)
	require.NoError(t, err)
	assert.Equal(t, EventPayoutCompleted, event.Event)

	ref, err := event.Resource()
	require.NoError(t, err)
	assert.Equal(t, "pay_kes_abc:pending", ref.DedupeKey())
	assert.Equal(t, "loan-1", ref.ExternalReference)

	p, err := event.Payout()
	require.NoError(t, err)
	assert.Equal(t, "1000.00", p.To.Amount.String())

	assert.NoError(t, v.Verify(id, ts, "v1,bm9wZQ== "+good, body), "any listed signature may match")

	stale := strconv.FormatInt(now.Add(-DefaultWebhookTolerance-time.Second).Unix(), 10)
	failures := []struct {
		name        string
		id, ts, sig string
		body        []byte
	}{
		{"tampered body", id, ts, good, append([]byte{}, append(body, ' ')...)},
		{"wrong id", "whd_2", ts, good, body},
		{"stale timestamp", id, stale, sign(t, key, id, stale, body), body},
		{"bad timestamp", id, "soon", good, body},
		{"missing signature", id, ts, "", body},
		{"unknown version", id, ts, "v2," + good[3:], body},
	}
	for _, f := range failures {
		t.Run(f.name, func(t *testing.T) {
			err := v.Verify(f.id, f.ts, f.sig, f.body)
			require.Error(t, err)
			assert.Equal(t, pkgErrors.CodeUnauthorized, codeOf(t, err))
		})
	}

	for _, bad := range []string{"", "abc", "whsec_", "whsec_!!!"} {
		_, err := NewWebhookVerifier(bad)
		assert.Error(t, err, bad)
	}
}

func TestListWebhookEvents(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, EventPayoutCompleted, r.URL.Query().Get("eventType"))
		assert.Equal(t, "50", r.URL.Query().Get("limit"))
		writeJSON(t, w, 200, `{"data":[{"id":"wev_1","eventType":"payout.completed","resourceId":"pay_kes_abc","payload":{"id":"pay_kes_abc","status":"completed"},"createdAt":"2026-11-27T10:00:00Z"}],"hasMore":false,"nextCursor":null}`)
	})

	page, err := c.ListWebhookEvents(context.Background(), EventFilter{EventType: EventPayoutCompleted, Limit: 50})
	require.NoError(t, err)
	require.Len(t, page.Data, 1)
	assert.Equal(t, "pay_kes_abc", page.Data[0].ResourceID)
}

func TestFindCryptoDeposit(t *testing.T) {
	since := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		assert.Equal(t, "/v1/collections", r.URL.Path)
		assert.Equal(t, CollectionTypeCrypto, q.Get("type"))
		assert.Equal(t, "addr_1", q.Get("cryptoAddressId"))
		assert.Equal(t, "2026-11-27T10:00:00Z", q.Get("from"))
		if q.Get("cursor") == "" {
			writeJSON(t, w, 200, `{"data":[{"id":"col_other","type":"crypto","status":"completed","from":{"accountNumber":"GX","code":null,"amount":"5","currency":"USDC"},"to":{"accountNumber":"USDC1","amount":"4.925","currency":"USDC"},"fee":null,"details":{"hash":"OTHER","chain":"Stellar","tag":"2802699348"},"externalReference":null,"createdAt":"2026-11-27T10:01:00Z"}],"hasMore":true,"nextCursor":"c2"}`)
			return
		}
		writeJSON(t, w, 200, `{"data":[{"id":"col_1","type":"crypto","status":"completed","from":{"accountNumber":"GX","code":null,"amount":"500","currency":"USDC"},"to":{"accountNumber":"USDC1","amount":"492.5","currency":"USDC"},"fee":{"amount":"7.5","currency":"USDC"},"details":{"hash":"abc123","chain":"Stellar","tag":"2802699348"},"externalReference":null,"createdAt":"2026-11-27T10:02:00Z"}],"hasMore":false,"nextCursor":null}`)
	})

	dep, ok, err := c.FindCryptoDeposit(context.Background(), "addr_1", "ABC123", since)
	require.NoError(t, err)
	require.True(t, ok, "hash matching ignores case and follows pages")
	assert.Equal(t, "col_1", dep.ID)
	assert.Equal(t, "7.5", dep.Fee.Amount.String())

	_, ok, err = c.FindCryptoDeposit(context.Background(), "addr_1", "missing", since)
	require.NoError(t, err)
	assert.False(t, ok)

	_, _, err = c.FindCryptoDeposit(context.Background(), "", "abc", since)
	require.Error(t, err)
}
