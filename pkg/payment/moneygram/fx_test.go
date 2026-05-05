package moneygram

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeFallback implements FallbackRateSource for tests. Each call returns
// (rate, err) — set both per case to drive different scenarios.
type fakeFallback struct {
	rate float64
	err  error
	hits int
}

func (f *fakeFallback) Get(_ context.Context, _ string) (float64, error) {
	f.hits++
	return f.rate, f.err
}

// newPrimaryReturning builds an FXRateClient whose underlying HTTP server
// always answers with a single CASH_PICKUP rate of `rate` (or HTTP 500 when
// rate <= 0).
func newPrimaryReturning(t *testing.T, rate float64) *FXRateClient {
	t.Helper()

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `{"access_token":"tok","expires_in":3600}`)
	}))
	t.Cleanup(tokenSrv.Close)

	rateSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if rate <= 0 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, `{"rates":[{"serviceOption":"CASH_PICKUP","rate":%f}]}`, rate)
	}))
	t.Cleanup(rateSrv.Close)

	oauth, err := NewOAuthClient(OAuthConfig{
		TokenURL: tokenSrv.URL, ClientID: "id", ClientSecret: "sec",
	}, tokenSrv.Client(), nil)
	require.NoError(t, err)

	fx, err := NewFXRateClient(FXRateConfig{BaseURL: rateSrv.URL, CacheTTL: 0}, oauth, rateSrv.Client(), nil)
	require.NoError(t, err)
	return fx
}

func TestFXOrchestrator_RejectsBothNil(t *testing.T) {
	_, err := NewFXOrchestrator(nil, nil, FXOrchestratorConfig{}, nil)
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestFXOrchestrator_PrimarySuccess_AppliesPrimaryBuffer(t *testing.T) {
	primary := newPrimaryReturning(t, 153.50)
	fb := &fakeFallback{rate: 999} // never used

	o, err := NewFXOrchestrator(primary, fb, FXOrchestratorConfig{
		EntryBufferPct: 0.01, EntryBufferPctFallback: 0.05,
	}, nil)
	require.NoError(t, err)

	got, err := o.Quote(context.Background(), FXQuoteRequest{
		OriginatingCountry: "USA", DestinationCountry: "KEN",
		SendCurrency: "USD", ReceiveCurrency: "KES",
	})
	require.NoError(t, err)
	assert.Equal(t, RateSourceMoneyGram, got.Source)
	assert.Equal(t, 0.01, got.BufferPct)
	assert.InDelta(t, 153.50*0.99, got.Rate, 0.01)
	assert.Equal(t, 0, fb.hits, "fallback must not be consulted on primary success")
}

func TestFXOrchestrator_PrimaryFail_FallsBackToFallback(t *testing.T) {
	primary := newPrimaryReturning(t, 0) // 500s
	fb := &fakeFallback{rate: 150.00}

	o, err := NewFXOrchestrator(primary, fb, FXOrchestratorConfig{
		EntryBufferPct: 0.01, EntryBufferPctFallback: 0.015,
	}, nil)
	require.NoError(t, err)

	got, err := o.Quote(context.Background(), FXQuoteRequest{
		OriginatingCountry: "USA", DestinationCountry: "KEN",
		SendCurrency: "USD", ReceiveCurrency: "KES",
	})
	require.NoError(t, err)
	assert.Equal(t, RateSourceFallback, got.Source)
	assert.Equal(t, 0.015, got.BufferPct)
	assert.InDelta(t, 150.00*0.985, got.Rate, 0.01)
	assert.Equal(t, 1, fb.hits)
}

func TestFXOrchestrator_BothFail_ServesStaleCache(t *testing.T) {
	// First call: primary OK → caches at 100.0 with 1% buffer = 99.0.
	primary := newPrimaryReturning(t, 100.0)
	fb := &fakeFallback{err: errors.New("yc down")}
	o, err := NewFXOrchestrator(primary, fb, FXOrchestratorConfig{
		EntryBufferPct: 0.01, StaleCacheMaxAge: time.Hour,
	}, nil)
	require.NoError(t, err)

	first, err := o.Quote(context.Background(), FXQuoteRequest{
		OriginatingCountry: "USA", DestinationCountry: "KEN",
		SendCurrency: "USD", ReceiveCurrency: "KES",
	})
	require.NoError(t, err)
	require.Equal(t, RateSourceMoneyGram, first.Source)

	// Knock out the primary so the next call must traverse the cascade.
	o.primary = newPrimaryReturning(t, 0)

	second, err := o.Quote(context.Background(), FXQuoteRequest{
		OriginatingCountry: "USA", DestinationCountry: "KEN",
		SendCurrency: "USD", ReceiveCurrency: "KES",
	})
	require.NoError(t, err)
	assert.Equal(t, RateSourceCachedPrimary, second.Source,
		"cache should remember the original source as 'cached_primary'")
	assert.InDelta(t, first.Rate, second.Rate, 0.0001)
}

func TestFXOrchestrator_AllFail_NoCache_ReturnsError(t *testing.T) {
	primary := newPrimaryReturning(t, 0)
	fb := &fakeFallback{err: errors.New("yc down")}
	o, err := NewFXOrchestrator(primary, fb, FXOrchestratorConfig{}, nil)
	require.NoError(t, err)

	_, err = o.Quote(context.Background(), FXQuoteRequest{
		OriginatingCountry: "USA", DestinationCountry: "KEN",
		SendCurrency: "USD", ReceiveCurrency: "KES",
	})
	require.ErrorIs(t, err, ErrNoRateAvailable)
}

func TestFXOrchestrator_StaleCache_BeyondMaxAge_Rejected(t *testing.T) {
	primary := newPrimaryReturning(t, 100.0)
	fb := &fakeFallback{err: errors.New("never")}
	o, err := NewFXOrchestrator(primary, fb, FXOrchestratorConfig{
		StaleCacheMaxAge: 1 * time.Millisecond,
	}, nil)
	require.NoError(t, err)

	// Prime the cache.
	_, err = o.Quote(context.Background(), FXQuoteRequest{
		OriginatingCountry: "USA", DestinationCountry: "KEN",
		SendCurrency: "USD", ReceiveCurrency: "KES",
	})
	require.NoError(t, err)

	// Wait for cache to age past MaxAge, then break primary.
	time.Sleep(5 * time.Millisecond)
	o.primary = newPrimaryReturning(t, 0)

	_, err = o.Quote(context.Background(), FXQuoteRequest{
		OriginatingCountry: "USA", DestinationCountry: "KEN",
		SendCurrency: "USD", ReceiveCurrency: "KES",
	})
	require.ErrorIs(t, err, ErrNoRateAvailable, "expired cache must not be served")
}

func TestFXOrchestrator_FallbackOnly(t *testing.T) {
	// MG primary not configured (e.g. REST credentials missing).
	fb := &fakeFallback{rate: 145.00}
	o, err := NewFXOrchestrator(nil, fb, FXOrchestratorConfig{
		EntryBufferPctFallback: 0.02,
	}, nil)
	require.NoError(t, err)

	got, err := o.Quote(context.Background(), FXQuoteRequest{
		SendCurrency: "USD", ReceiveCurrency: "KES",
	})
	require.NoError(t, err)
	assert.Equal(t, RateSourceFallback, got.Source)
	assert.InDelta(t, 145.00*0.98, got.Rate, 0.01)
}

func TestFXOrchestrator_RejectsEmptyCurrencies(t *testing.T) {
	primary := newPrimaryReturning(t, 100.0)
	o, err := NewFXOrchestrator(primary, nil, FXOrchestratorConfig{}, nil)
	require.NoError(t, err)

	_, err = o.Quote(context.Background(), FXQuoteRequest{})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestFXOrchestrator_ValidateAmount(t *testing.T) {
	o, err := NewFXOrchestrator(newPrimaryReturning(t, 100), nil, FXOrchestratorConfig{
		MinUSD: 5, MaxUSD: 1000,
	}, nil)
	require.NoError(t, err)

	require.NoError(t, o.ValidateAmount(5))
	require.NoError(t, o.ValidateAmount(500))
	require.NoError(t, o.ValidateAmount(1000))

	require.ErrorIs(t, o.ValidateAmount(4.99), ErrAmountOutOfRange)
	require.ErrorIs(t, o.ValidateAmount(1000.01), ErrAmountOutOfRange)
}

func TestApplyBuffer(t *testing.T) {
	tests := []struct {
		rate, buf, want float64
	}{
		{100, 0, 100},
		{100, 0.01, 99},
		{100, 0.05, 95},
		{0, 0.5, 0},
		{100, 1.5, 0}, // clamped, never negative
	}
	for _, tc := range tests {
		assert.InDelta(t, tc.want, applyBuffer(tc.rate, tc.buf), 0.0001)
	}
}
