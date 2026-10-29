package didit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/compliance"
)

func sign(secret string, payload []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(payload)
	return hex.EncodeToString(m.Sum(nil))
}

func TestCanonicalMatchesDiditsForm(t *testing.T) {
	body := []byte(`{"z":1,"a":{"score":100.0,"ratio":0.25,"name":"Kāmau \"K\" Ltd"},"list":[2.000,"x"],"n":null,"ok":true}`)
	got, err := Canonical(body)
	require.NoError(t, err)
	assert.Equal(t, `{"a":{"name":"Kāmau \"K\" Ltd","ratio":0.25,"score":100},"list":[2,"x"],"n":null,"ok":true,"z":1}`, string(got))
}

func TestVerifyWebhook(t *testing.T) {
	const secret = "whsec"
	now := time.Unix(1_800_000_000, 0)
	ts := strconv.FormatInt(now.Unix(), 10)
	body := []byte(`{"event":"status.updated","timestamp":1800000000,"data":{"session_id":"bs_1","session_kind":"business","vendor_data":"cp-1","status":"APPROVED","previous_status":"IN_PROGRESS","score":91.0}}`)
	canonical, err := Canonical(body)
	require.NoError(t, err)

	ev, err := VerifyWebhook(secret, body, WebhookHeaders{Timestamp: ts, SignatureV2: sign(secret, canonical)}, now)
	require.NoError(t, err)
	assert.Equal(t, "APPROVED", ev.Data.Status)
	assert.Equal(t, "business", ev.Data.SessionKind)
	assert.Equal(t, "cp-1", ev.Data.VendorData)

	_, err = VerifyWebhook(secret, body, WebhookHeaders{Timestamp: ts, SignatureV2: "deadbeef", PreviousV2: sign("old", canonical)}, now)
	assert.True(t, errors.Is(err, ErrInvalidSignature), "a signature with another secret must fail")
	_, err = VerifyWebhook("old", body, WebhookHeaders{Timestamp: ts, SignatureV2: "deadbeef", PreviousV2: sign("old", canonical)}, now)
	assert.NoError(t, err, "the previous signature is accepted during rotation")

	_, err = VerifyWebhook(secret, body, WebhookHeaders{Timestamp: ts, Signature: sign(secret, body)}, now)
	assert.NoError(t, err, "raw-body signature is the fallback")

	_, err = VerifyWebhook(secret, body, WebhookHeaders{Timestamp: ts, SignatureV2: sign(secret, canonical)}, now.Add(301*time.Second))
	assert.True(t, errors.Is(err, ErrStaleWebhook))

	tampered := []byte(`{"event":"status.updated","data":{"status":"DECLINED"}}`)
	_, err = VerifyWebhook(secret, tampered, WebhookHeaders{Timestamp: ts, SignatureV2: sign(secret, canonical)}, now)
	assert.True(t, errors.Is(err, ErrInvalidSignature))
}

func TestScreenAddressRefusesStellarWithoutCalling(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer srv.Close()
	c := NewClient(Config{APIKey: "k", BaseURL: srv.URL, Chain: "stellar"})
	_, err := c.ScreenAddress(context.Background(), compliance.ScreenRequest{Address: "GABC"})
	assert.True(t, errors.Is(err, ErrUnsupportedChain))
	assert.False(t, called)
	assert.False(t, Supports("stellar"))
	assert.True(t, Supports("ethereum"))
}

func TestScreenAddressOnASupportedChain(t *testing.T) {
	var got walletRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v3/wallet-screening/", r.URL.Path)
		assert.Equal(t, "k", r.Header.Get("x-api-key"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		_, _ = w.Write([]byte(`{"provider":"merklescience","risk_score":95,"severity":"CRITICAL","status":"SCREENED","sanctions_hit":true}`))
	}))
	defer srv.Close()
	c := NewClient(Config{APIKey: "k", BaseURL: srv.URL, Chain: "ethereum", Thresholds: Thresholds{Review: 40, Reject: 90}})
	s, err := c.ScreenAddress(context.Background(), compliance.ScreenRequest{Address: "0xabc"})
	require.NoError(t, err)
	assert.Equal(t, "ETH", got.Blockchain)
	assert.Equal(t, compliance.VerdictRejected, s.Verdict)
	assert.Equal(t, "merklescience", s.ProviderRef)
	assert.True(t, s.Sanctioned)
}

func TestScreeningVerdicts(t *testing.T) {
	score := func(n int) *int { return &n }
	th := Thresholds{Review: 40, Reject: 90}
	assert.Equal(t, compliance.VerdictApproved, deriveVerdict(walletResponse{Status: "SCREENED", RiskScore: score(15)}, th))
	assert.Equal(t, compliance.VerdictReview, deriveVerdict(walletResponse{Status: "SCREENED", RiskScore: score(55)}, th))
	assert.Equal(t, compliance.VerdictPending, deriveVerdict(walletResponse{Status: "PENDING", RiskScore: score(0)}, th))
	assert.Equal(t, compliance.VerdictReview, deriveVerdict(walletResponse{Status: "SCREENED", RiskScore: score(0)}, Thresholds{}), "unset thresholds never approve")
}

func TestCreateKYBSession(t *testing.T) {
	var got createSessionRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v3/session/", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		_, _ = w.Write([]byte(`{"session_id":"bs_1","session_number":42,"url":"https://verify.didit.me/session/bs_1"}`))
	}))
	defer srv.Close()
	c := NewClient(Config{APIKey: "k", BaseURL: srv.URL})
	s, err := c.CreateKYBSession(context.Background(), "wf-kyb", "cp-1", "")
	require.NoError(t, err)
	assert.Equal(t, "wf-kyb", got.WorkflowID)
	assert.Equal(t, "cp-1", got.VendorData)
	assert.Equal(t, "bs_1", s.ID)
	assert.Equal(t, "https://verify.didit.me/session/bs_1", s.URL)
}
