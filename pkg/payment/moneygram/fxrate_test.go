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

func newTestFXClient(t *testing.T, handler http.HandlerFunc, ttl time.Duration) (*FXRateClient, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `{"access_token":"tok","expires_in":3600}`)
	}))
	t.Cleanup(tokenSrv.Close)

	oauth, err := NewOAuthClient(OAuthConfig{
		TokenURL: tokenSrv.URL, ClientID: "id", ClientSecret: "sec",
	}, tokenSrv.Client(), nil)
	require.NoError(t, err)

	fx, err := NewFXRateClient(FXRateConfig{BaseURL: srv.URL, CacheTTL: ttl}, oauth, srv.Client(), nil)
	require.NoError(t, err)
	return fx, srv
}

func TestFXRateClient_FiltersServiceOption(t *testing.T) {
	var calls int32
	fx, _ := newTestFXClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		assert.Equal(t, "USA", r.URL.Query().Get("originatingCountry"))
		assert.Equal(t, "USD", r.URL.Query().Get("sendCurrency"))
		assert.Equal(t, "KEN", r.URL.Query().Get("destinationCountry"))
		assert.Equal(t, "tok", r.Header.Get("access_token"))

		fmt.Fprintln(w, `{"rates":[
			{"serviceOption":"BANK_DEPOSIT","sendCurrency":"USD","receiveCurrency":"KES","rate":129.10},
			{"serviceOption":"CASH_PICKUP","sendCurrency":"USD","receiveCurrency":"KES","rate":128.50},
			{"serviceOption":"MOBILE_WALLET","sendCurrency":"USD","receiveCurrency":"KES","rate":128.95}
		]}`)
	}, 0)

	got, err := fx.Get(context.Background(), FXRateRequest{
		OriginatingCountry: "USA",
		SendCurrency:       "USD",
		DestinationCountry: "KEN",
		ServiceOption:      ServiceOptionCashPickup,
	})
	require.NoError(t, err)
	assert.Equal(t, ServiceOptionCashPickup, got.ServiceOption)
	assert.Equal(t, 128.50, got.Rate)
	assert.False(t, got.FetchedAt.IsZero())
}

func TestFXRateClient_CacheHit(t *testing.T) {
	var calls int32
	fx, _ := newTestFXClient(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		fmt.Fprintln(w, `{"rates":[{"serviceOption":"CASH_PICKUP","rate":128.50}]}`)
	}, 5*time.Minute)

	req := FXRateRequest{
		OriginatingCountry: "USA", SendCurrency: "USD", DestinationCountry: "KEN",
		ServiceOption: ServiceOptionCashPickup,
	}
	_, err := fx.Get(context.Background(), req)
	require.NoError(t, err)
	_, err = fx.Get(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "second Get with cache TTL should be served from cache")
}

func TestFXRateClient_ServiceOptionUnavailable(t *testing.T) {
	fx, _ := newTestFXClient(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `{"rates":[{"serviceOption":"BANK_DEPOSIT","rate":129.10}]}`)
	}, 0)

	_, err := fx.Get(context.Background(), FXRateRequest{
		OriginatingCountry: "USA", SendCurrency: "USD", DestinationCountry: "KEN",
		ServiceOption: ServiceOptionCashPickup,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrServiceOptionUnavailable)
}

func TestFXRateClient_PropagatesUnauthorized(t *testing.T) {
	fx, _ := newTestFXClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}, 0)

	_, err := fx.Get(context.Background(), FXRateRequest{
		OriginatingCountry: "USA", SendCurrency: "USD", DestinationCountry: "KEN",
		ServiceOption: ServiceOptionCashPickup,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnauthorized)
}

func TestFXRateClient_RejectsBadRequest(t *testing.T) {
	fx, _ := newTestFXClient(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("server should not be hit on validation failure")
	}, 0)

	_, err := fx.Get(context.Background(), FXRateRequest{ServiceOption: "x"})
	require.ErrorIs(t, err, ErrInvalidConfig)
}
