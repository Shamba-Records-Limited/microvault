package moneygram

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stellar/go-stellar-sdk/txnbuild"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testNetworkPassphrase = "Test SDF Network ; September 2015"

// fakeAuthServer mimics MoneyGram's SEP-10 endpoints. The server keypair is
// generated per-test so the SDK's signature verification has a real key to
// validate against.
type fakeAuthServer struct {
	t          *testing.T
	serverKP   *keypair.Full
	homeDomain string
	issuedJWT  string

	server *httptest.Server
}

func newFakeAuthServer(t *testing.T, homeDomain, jwt string) *fakeAuthServer {
	t.Helper()
	kp, err := keypair.Random()
	require.NoError(t, err)
	f := &fakeAuthServer{t: t, serverKP: kp, homeDomain: homeDomain, issuedJWT: jwt}

	mux := http.NewServeMux()
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			f.serveChallenge(w, r)
		case http.MethodPost:
			f.serveToken(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeAuthServer) serveChallenge(w http.ResponseWriter, r *http.Request) {
	clientAccount := r.URL.Query().Get("account")
	memoStr := r.URL.Query().Get("memo")
	require.NotEmpty(f.t, clientAccount, "challenge GET must include account")
	require.NotEmpty(f.t, memoStr, "challenge GET must include memo")

	memo := txnbuild.MemoID(0)
	_, err := fmt.Sscanf(memoStr, "%d", &memo)
	require.NoError(f.t, err)

	tx, err := txnbuild.BuildChallengeTx(
		f.serverKP.Seed(),
		clientAccount,
		f.homeDomain,
		f.homeDomain,
		testNetworkPassphrase,
		5*time.Minute,
		&memo,
	)
	require.NoError(f.t, err)

	xdrStr, err := tx.Base64()
	require.NoError(f.t, err)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"transaction":        xdrStr,
		"network_passphrase": testNetworkPassphrase,
	})
}

func (f *fakeAuthServer) serveToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Transaction string `json:"transaction"`
	}
	require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))
	require.NotEmpty(f.t, body.Transaction)

	// Sanity: the submitted XDR should now contain two signatures (server +
	// client). We don't strictly need to re-verify here — that's the SDK's
	// job — but we assert the wallet at least added bytes.
	parsed, err := txnbuild.TransactionFromXDR(body.Transaction)
	require.NoError(f.t, err)
	tx, ok := parsed.Transaction()
	require.True(f.t, ok)
	require.GreaterOrEqual(f.t, len(tx.Signatures()), 2, "client should add a signature")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"token": f.issuedJWT})
}

// makeJWT builds a minimal JWT payload with a given exp; signature is empty
// because tokenExpiry doesn't verify, only parses.
func makeJWT(exp int64) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payloadBytes, _ := json.Marshal(map[string]int64{"exp": exp})
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	return header + "." + payload + "."
}

func TestAuthClient_RoundTrip(t *testing.T) {
	homeDomain := "stellar.moneygram.test"
	want := makeJWT(time.Now().Add(time.Hour).Unix())
	srv := newFakeAuthServer(t, homeDomain, want)

	clientKP, err := keypair.Random()
	require.NoError(t, err)

	auth, err := NewAuthClient(AuthConfig{
		WebAuthEndpoint:   srv.server.URL + "/auth",
		ServerSigningKey:  srv.serverKP.Address(),
		NetworkPassphrase: testNetworkPassphrase,
		HomeDomain:        homeDomain,
		TreasurySecret:    clientKP.Seed(),
	}, srv.server.Client(), nil)
	require.NoError(t, err)

	res, err := auth.Authenticate(context.Background(), 12345)
	require.NoError(t, err)
	assert.Equal(t, want, res.JWT)
	assert.True(t, res.ExpiresAt.After(time.Now().Add(30*time.Minute)),
		"ExpiresAt should be parsed from the JWT exp claim")
	assert.Equal(t, clientKP.Address(), auth.TreasuryAddress())
}

func TestAuthClient_RejectsWrongServerKey(t *testing.T) {
	homeDomain := "stellar.moneygram.test"
	srv := newFakeAuthServer(t, homeDomain, makeJWT(time.Now().Add(time.Hour).Unix()))

	clientKP, err := keypair.Random()
	require.NoError(t, err)

	// Plug in an unrelated server signing key — challenge validation must fail.
	wrongKP, err := keypair.Random()
	require.NoError(t, err)

	auth, err := NewAuthClient(AuthConfig{
		WebAuthEndpoint:   srv.server.URL + "/auth",
		ServerSigningKey:  wrongKP.Address(),
		NetworkPassphrase: testNetworkPassphrase,
		HomeDomain:        homeDomain,
		TreasurySecret:    clientKP.Seed(),
	}, srv.server.Client(), nil)
	require.NoError(t, err)

	_, err = auth.Authenticate(context.Background(), 1)
	require.Error(t, err)
}

func TestAuthClient_RejectsBadConfig(t *testing.T) {
	clientKP, err := keypair.Random()
	require.NoError(t, err)

	tests := []AuthConfig{
		{},
		{WebAuthEndpoint: "u"},
		{WebAuthEndpoint: "u", ServerSigningKey: "k"},
		{WebAuthEndpoint: "u", ServerSigningKey: "k", NetworkPassphrase: "n"},
		{WebAuthEndpoint: "u", ServerSigningKey: "k", NetworkPassphrase: "n", HomeDomain: "h"},
		// Bad treasury secret format
		{WebAuthEndpoint: "u", ServerSigningKey: "k", NetworkPassphrase: "n", HomeDomain: "h", TreasurySecret: "not-a-stellar-secret"},
	}
	for i, cfg := range tests {
		_, err := NewAuthClient(cfg, nil, nil)
		require.ErrorIs(t, err, ErrInvalidConfig, "case %d", i)
	}

	// Sanity: the same fields with a real seed should construct.
	good := AuthConfig{
		WebAuthEndpoint:   "https://example.com/auth",
		ServerSigningKey:  "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		NetworkPassphrase: testNetworkPassphrase,
		HomeDomain:        "example.com",
		TreasurySecret:    clientKP.Seed(),
	}
	_, err = NewAuthClient(good, nil, nil)
	require.NoError(t, err)
}

func TestTokenExpiry_FallsBackOnMalformed(t *testing.T) {
	fb := time.Now().Add(time.Hour)

	// Not a JWT at all.
	got := tokenExpiry("not-a-jwt", fb)
	assert.Equal(t, fb, got)

	// Three parts but middle isn't valid base64.
	got = tokenExpiry("aaa.???.bbb", fb)
	assert.Equal(t, fb, got)

	// Valid base64 but no exp claim.
	noExp := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"nobody"}`))
	got = tokenExpiry("a."+noExp+".b", fb)
	assert.Equal(t, fb, got)
}
