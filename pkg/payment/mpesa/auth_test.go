package mpesa

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeDaraja struct {
	mints     atomic.Int64
	apiCalls  atomic.Int64
	expiresIn string
	token     string
	apiStatus int
	apiBody   string
	server    *httptest.Server
}

func newFakeDaraja(t *testing.T) *fakeDaraja {
	t.Helper()
	f := &fakeDaraja{expiresIn: `"3599"`, token: "tok-1", apiStatus: 200, apiBody: `{"ok":true}`}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/oauth/") {
			f.mints.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"` + f.token + `","expires_in":` + f.expiresIn + `}`))
			return
		}
		f.apiCalls.Add(1)
		w.WriteHeader(f.apiStatus)
		_, _ = w.Write([]byte(f.apiBody))
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeDaraja) client(t *testing.T, clock func() time.Time) *Client {
	t.Helper()
	c, err := New(Config{
		Environment:    EnvironmentSandbox,
		ConsumerKey:    "consumer-key",
		ConsumerSecret: "consumer-secret",
		BaseURL:        f.server.URL,
		Clock:          clock,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestAccessToken_ExpiresInShapes(t *testing.T) {
	cases := map[string]struct {
		raw  string
		want int64
	}{
		"quoted string": {`"3599"`, 3599},
		"bare number":   {`3599`, 3599},
		"absent":        {`null`, 3599},
		"empty string":  {`""`, 3599},
		"garbage":       {`"soon"`, 3599},
		"negative":      {`"-5"`, 3599},
		"short":         {`"120"`, 120},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var body authResponse
			if err := json.Unmarshal([]byte(`{"access_token":"t","expires_in":`+tc.raw+`}`), &body); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := body.seconds(); got != tc.want {
				t.Errorf("seconds() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestAccessToken_CachesUntilExpiry(t *testing.T) {
	f := newFakeDaraja(t)
	now := time.Now()
	c := f.client(t, func() time.Time { return now })

	for range 5 {
		if _, err := c.AccessToken(context.Background()); err != nil {
			t.Fatalf("AccessToken: %v", err)
		}
	}
	if got := f.mints.Load(); got != 1 {
		t.Errorf("mints = %d, want 1", got)
	}

	now = now.Add(3599 * time.Second)
	if _, err := c.AccessToken(context.Background()); err != nil {
		t.Fatalf("AccessToken after expiry: %v", err)
	}
	if got := f.mints.Load(); got != 2 {
		t.Errorf("mints after expiry = %d, want 2", got)
	}
}

// Daraja invalidates the previous token on every mint, so concurrent callers
// must collapse into one. Without the single-flight, N callers mint N tokens
// and N-1 of them are dead on arrival.
func TestAccessToken_ConcurrentCallersMintOnce(t *testing.T) {
	f := newFakeDaraja(t)
	c := f.client(t, time.Now)

	var wg sync.WaitGroup
	tokens := make([]string, 32)
	for i := range tokens {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := c.AccessToken(context.Background())
			if err != nil {
				t.Errorf("AccessToken: %v", err)
				return
			}
			tokens[i] = token
		}()
	}
	wg.Wait()

	if got := f.mints.Load(); got != 1 {
		t.Errorf("mints = %d, want 1", got)
	}
	for i, token := range tokens {
		if token != "tok-1" {
			t.Errorf("caller %d got token %q", i, token)
		}
	}
}

// A token rejected by Daraja is retried exactly once after eviction. A loop
// would mask a genuine credential failure behind an unbounded retry.
func TestCall_RetriesRejectedTokenExactlyOnce(t *testing.T) {
	f := newFakeDaraja(t)
	f.apiStatus = http.StatusNotFound
	f.apiBody = `{"requestId":"r","errorCode":"404.001.03","errorMessage":"Invalid Access Token"}`
	c := f.client(t, time.Now)

	type payload struct{ OK bool }
	_, err := call[payload](context.Background(), c, mpesaErr("test"), http.MethodPost, "/mpesa/test", map[string]string{"a": "b"})
	if err == nil {
		t.Fatal("expected the second rejection to surface")
	}
	if got := f.apiCalls.Load(); got != 2 {
		t.Errorf("api calls = %d, want 2", got)
	}
	if got := f.mints.Load(); got != 2 {
		t.Errorf("mints = %d, want 2 (initial plus one re-mint)", got)
	}
}

func TestCall_DoesNotRetryOtherFailures(t *testing.T) {
	f := newFakeDaraja(t)
	f.apiStatus = http.StatusBadRequest
	f.apiBody = `{"errorCode":"400.002.02","errorMessage":"Bad Request - Invalid Amount"}`
	c := f.client(t, time.Now)

	type payload struct{ OK bool }
	if _, err := call[payload](context.Background(), c, mpesaErr("test"), http.MethodPost, "/mpesa/test", nil); err == nil {
		t.Fatal("expected an error")
	}
	if got := f.apiCalls.Load(); got != 1 {
		t.Errorf("api calls = %d, want 1", got)
	}
}

func TestCall_SendsBearerToken(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/oauth/") {
			_, _ = w.Write([]byte(`{"access_token":"tok-9","expires_in":"3599"}`))
			return
		}
		seen = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c, _ := New(Config{Environment: EnvironmentSandbox, ConsumerKey: "k", ConsumerSecret: "s", BaseURL: srv.URL})
	type payload struct{ OK bool }
	if _, err := call[payload](context.Background(), c, mpesaErr("test"), http.MethodPost, "/mpesa/test", nil); err != nil {
		t.Fatalf("call: %v", err)
	}
	if seen != "Bearer tok-9" {
		t.Errorf("Authorization = %q", seen)
	}
}

func TestMintToken_EmptyTokenIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"","expires_in":"3599"}`))
	}))
	defer srv.Close()

	c, _ := New(Config{Environment: EnvironmentSandbox, ConsumerKey: "k", ConsumerSecret: "s", BaseURL: srv.URL})
	if _, err := c.AccessToken(context.Background()); err == nil {
		t.Error("expected an error when Daraja returns no token")
	}
}

// The cache key is shared infrastructure in a multi-replica deployment, so it
// must not carry the consumer key.
func TestTokenKey_DoesNotLeakCredentials(t *testing.T) {
	c, _ := New(Config{Environment: EnvironmentSandbox, ConsumerKey: "super-secret-key", ConsumerSecret: "s"})
	key := c.tokenKey()

	if strings.Contains(key, "super-secret-key") {
		t.Errorf("token key contains the consumer key: %q", key)
	}
	if !strings.HasPrefix(key, "mpesa:token:sandbox:") {
		t.Errorf("token key = %q", key)
	}

	other, _ := New(Config{Environment: EnvironmentSandbox, ConsumerKey: "another-key", ConsumerSecret: "s"})
	if other.tokenKey() == key {
		t.Error("different consumer keys produced the same cache key")
	}
	production, _ := New(Config{Environment: EnvironmentProduction, ConsumerKey: "super-secret-key", ConsumerSecret: "s"})
	if production.tokenKey() == key {
		t.Error("sandbox and production share a cache key")
	}
}

func TestMemoryTokenStore(t *testing.T) {
	store := NewMemoryTokenStore()
	ctx := context.Background()

	if _, _, ok := store.Get(ctx, "absent"); ok {
		t.Error("expected a miss")
	}

	expiry := time.Now().Add(time.Hour)
	if err := store.Set(ctx, "k", "t", expiry); err != nil {
		t.Fatalf("Set: %v", err)
	}
	token, got, ok := store.Get(ctx, "k")
	if !ok || token != "t" || !got.Equal(expiry) {
		t.Errorf("Get = %q, %v, %v", token, got, ok)
	}

	if err := store.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, ok := store.Get(ctx, "k"); ok {
		t.Error("expected a miss after Delete")
	}
}
