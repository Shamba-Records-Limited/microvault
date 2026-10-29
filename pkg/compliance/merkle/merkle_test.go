package merkle

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/compliance"
)

const lender = "GCUUFSK3P7YMFMYVA3L7LVQ7TCWJK7WUOFMXLX2HYSRCFB37ZGZD4CCX"

func server(t *testing.T, status int, body string, got *screenRequest, path *string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-key", r.Header.Get("X-API-KEY"))
		if path != nil {
			*path = r.URL.Path
		}
		if got != nil {
			require.NoError(t, json.NewDecoder(r.Body).Decode(got))
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return NewClient(Config{APIKey: "test-key", BaseURL: srv.URL, Thresholds: Thresholds{Review: 3, Reject: 5}})
}

func TestScreenAddressSendsStellarAndCustomer(t *testing.T) {
	var got screenRequest
	var path string
	c := server(t, http.StatusOK, `{"identifier":"`+lender+`","risk_level":1,"risk_level_verbose":"Info",
		"earliest_transaction_time":"2026-09-01T00:00:00Z","originator":[{"tag_type_verbose":"Exchange","exposure_type":"direct"}]}`, &got, &path)

	s, err := c.ScreenAddress(context.Background(), compliance.ScreenRequest{Address: lender, CustomerReference: "kyb-123"})
	require.NoError(t, err)
	assert.Equal(t, "/addresses/", path)
	assert.Equal(t, "13", got.Blockchain, "Stellar is blockchain code 13")
	assert.Equal(t, lender, got.Identifier)
	assert.Equal(t, "kyb-123", got.CustomerID)
	assert.Equal(t, typeDeposit, got.Type)
	assert.Equal(t, compliance.VerdictApproved, s.Verdict)
	assert.Equal(t, Provider, s.Provider)
	require.NotNil(t, s.RiskScore)
	assert.InDelta(t, 1, *s.RiskScore, 0)
	assert.NotEmpty(t, s.Raw)
}

func TestVerdicts(t *testing.T) {
	cases := map[string]struct {
		body string
		want compliance.Verdict
	}{
		"own sanctions tag":           {`{"risk_level":2,"earliest_transaction_time":"x","tags":{"owner":{"tag_type_verbose":"Sanctions"}}}`, compliance.VerdictRejected},
		"direct sanctions exposure":   {`{"risk_level":1,"earliest_transaction_time":"x","originator":[{"tag_type_verbose":"Sanctions","exposure_type":"direct"}]}`, compliance.VerdictRejected},
		"indirect sanctions exposure": {`{"risk_level":1,"earliest_transaction_time":"x","originator":[{"tag_type_verbose":"Sanctions","exposure_type":"indirect"}]}`, compliance.VerdictReview},
		"critical":                    {`{"risk_level":5,"earliest_transaction_time":"x"}`, compliance.VerdictRejected},
		"medium":                      {`{"risk_level":3,"earliest_transaction_time":"x"}`, compliance.VerdictReview},
		"no history":                  {`{"risk_level":0}`, compliance.VerdictUnscreenable},
		"no level":                    {`{"earliest_transaction_time":"x"}`, compliance.VerdictPending},
	}
	for name, c := range cases {
		cl := server(t, http.StatusOK, c.body, nil, nil)
		s, err := cl.ScreenAddress(context.Background(), compliance.ScreenRequest{Address: lender})
		require.NoError(t, err, name)
		assert.Equal(t, c.want, s.Verdict, name)
	}
}

func TestUnsetThresholdsNeverApprove(t *testing.T) {
	r := screenResponse{RiskLevel: new(int), EarliestTxTime: new(string)}
	assert.Equal(t, compliance.VerdictReview, deriveVerdict(r, Thresholds{}, true))
}

func TestNotSupportedIsTyped(t *testing.T) {
	c := server(t, http.StatusBadRequest, `{"error":["Token Not Supported or Invalid Receiver Address"]}`, nil, nil)
	_, err := c.ScreenAddress(context.Background(), compliance.ScreenRequest{Address: lender})
	assert.True(t, errors.Is(err, ErrNotSupported))
}

func TestScreenTransaction(t *testing.T) {
	var got screenRequest
	var path string
	c := server(t, http.StatusOK, `{"risk_level":5,"risk_level_verbose":"Critical","originator":[{"tag_type_verbose":"Sanctions","exposure_type":"direct"}]}`, &got, &path)
	s, err := c.ScreenTransaction(context.Background(), compliance.TxScreenRequest{Hash: "abc", Address: lender})
	require.NoError(t, err)
	assert.Equal(t, "/transactions/", path)
	assert.Equal(t, "abc", got.Identifier)
	assert.Equal(t, lender, got.Address)
	assert.Equal(t, "abc", s.TxHash)
	assert.Equal(t, compliance.VerdictRejected, s.Verdict)
	assert.True(t, s.Sanctioned)
}
