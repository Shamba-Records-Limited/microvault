package moneygram

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOAuthClient_FetchesAndCachesToken(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "client_credentials", r.Form.Get("grant_type"))
		assert.Equal(t, "abc", r.Form.Get("client_id"))
		assert.Equal(t, "secret", r.Form.Get("client_secret"))
		assert.Equal(t, "fx_rate", r.Form.Get("scope"))

		fmt.Fprintln(w, `{"access_token":"tok-1","token_type":"Bearer","expires_in":3600}`)
	}))
	defer srv.Close()

	c, err := NewOAuthClient(OAuthConfig{
		TokenURL:     srv.URL,
		ClientID:     "abc",
		ClientSecret: "secret",
		Scope:        "fx_rate",
	}, srv.Client(), nil)
	require.NoError(t, err)

	t1, err := c.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "tok-1", t1)

	// Second call hits the cache, not the server.
	t2, err := c.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "tok-1", t2)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestOAuthClient_RefreshOnInvalidate(t *testing.T) {
	var seq int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&seq, 1)
		fmt.Fprintf(w, `{"access_token":"tok-%d","token_type":"Bearer","expires_in":3600}`, n)
	}))
	defer srv.Close()

	c, err := NewOAuthClient(OAuthConfig{TokenURL: srv.URL, ClientID: "x", ClientSecret: "y"}, srv.Client(), nil)
	require.NoError(t, err)

	t1, _ := c.Token(context.Background())
	assert.Equal(t, "tok-1", t1)

	c.Invalidate()
	t2, _ := c.Token(context.Background())
	assert.Equal(t, "tok-2", t2)
}

func TestOAuthClient_RefreshNearExpiry(t *testing.T) {
	var seq int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&seq, 1)
		// Token nominally expires in 1s; safety margin of 30s should treat it
		// as already stale on the very next call.
		fmt.Fprintf(w, `{"access_token":"tok-%d","token_type":"Bearer","expires_in":1}`, n)
	}))
	defer srv.Close()

	c, err := NewOAuthClient(OAuthConfig{TokenURL: srv.URL, ClientID: "x", ClientSecret: "y"}, srv.Client(), nil)
	require.NoError(t, err)

	t1, _ := c.Token(context.Background())
	t2, _ := c.Token(context.Background())
	assert.Equal(t, "tok-1", t1)
	assert.Equal(t, "tok-2", t2, "near-expiry token must be refreshed")
}

func TestOAuthClient_RejectsBadConfig(t *testing.T) {
	tests := []OAuthConfig{
		{},
		{TokenURL: "u"},
		{TokenURL: "u", ClientID: "id"},
	}
	for _, cfg := range tests {
		_, err := NewOAuthClient(cfg, nil, nil)
		require.ErrorIs(t, err, ErrInvalidConfig)
	}
}

func TestOAuthClient_PropagatesUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintln(w, `{"error":"invalid_client"}`)
	}))
	defer srv.Close()

	c, err := NewOAuthClient(OAuthConfig{TokenURL: srv.URL, ClientID: "x", ClientSecret: "y"}, srv.Client(), nil)
	require.NoError(t, err)

	_, err = c.Token(context.Background())
	require.ErrorIs(t, err, ErrUnauthorized)
}

func TestOAuthClient_DefaultsExpiry(t *testing.T) {
	// Server returns expires_in=0 — defensively, we should default to 1h
	// rather than treat as already-expired and hammer the endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `{"access_token":"tok","token_type":"Bearer","expires_in":0}`)
	}))
	defer srv.Close()

	c, err := NewOAuthClient(OAuthConfig{TokenURL: srv.URL, ClientID: "x", ClientSecret: "y"}, srv.Client(), nil)
	require.NoError(t, err)

	_, err = c.Token(context.Background())
	require.NoError(t, err)

	c.mu.Lock()
	defer c.mu.Unlock()
	assert.True(t, c.expires.After(time.Now().Add(30*time.Minute)),
		"expires_in=0 should fall back to ~1h, got %v", time.Until(c.expires))
}
