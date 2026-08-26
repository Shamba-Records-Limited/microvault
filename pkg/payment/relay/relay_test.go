package relay

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var oopsErr oops.OopsError
	require.True(t, errors.As(err, &oopsErr), "not an oops error: %v", err)
	code, _ := oopsErr.Code().(string)
	return code
}

// fakeSource returns a fixed rate per direction, or an error.
type fakeSource struct {
	name  string
	rates map[string]float64
	err   error
	calls atomic.Int32
}

func (f *fakeSource) Name() string { return f.name }

func (f *fakeSource) QuoteRate(_ context.Context, req RateRequest) (*RateQuote, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	rate, ok := f.rates[req.Direction]
	if !ok {
		return nil, errors.New("no rate for direction")
	}
	return &RateQuote{
		Provider:      f.name,
		Direction:     req.Direction,
		FiatCurrency:  req.FiatCurrency,
		CryptoAmount:  req.CryptoAmount,
		FiatAmount:    req.CryptoAmount * rate,
		EffectiveRate: rate,
	}, nil
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newRouter(t *testing.T, enabled bool, sources ...RateSource) *Router {
	t.Helper()
	r, err := NewWithSources(Config{
		Enabled: enabled,
		Default: providerA,
		Logger:  quietLogger(),
	}, sources...)
	require.NoError(t, err)
	return r
}

// Provider names are opaque to the Router; these stand in for any two.
const (
	providerA = "provider-a"
	providerB = "provider-b"
)

func kesRequest(direction string) RateRequest {
	return RateRequest{Direction: direction, FiatCurrency: "KES", CountryCode: "KE", CryptoAmount: 20}
}

// Off-ramp wants the most shillings per USDC; on-ramp wants the fewest.
func TestBest_PicksByDirection(t *testing.T) {
	yc := &fakeSource{name: providerA, rates: map[string]float64{
		DirectionOffRamp: 120, DirectionOnRamp: 130,
	}}
	fb := &fakeSource{name: providerB, rates: map[string]float64{
		DirectionOffRamp: 123.4, DirectionOnRamp: 132.65,
	}}
	router := newRouter(t, true, yc, fb)

	sell, err := router.Best(context.Background(), kesRequest(DirectionOffRamp))
	require.NoError(t, err)
	assert.Equal(t, providerB, sell.Provider, "higher yield wins an off-ramp")
	assert.Equal(t, 123.4, sell.EffectiveRate)

	buy, err := router.Best(context.Background(), kesRequest(DirectionOnRamp))
	require.NoError(t, err)
	assert.Equal(t, providerA, buy.Provider, "lower cost wins an on-ramp")
	assert.Equal(t, 130.0, buy.EffectiveRate)
}

// The switch is a kill switch: off must route exactly as before and must not
// even ask the other providers.
func TestBest_DisabledUsesDefaultOnly(t *testing.T) {
	yc := &fakeSource{name: providerA, rates: map[string]float64{DirectionOffRamp: 120}}
	fb := &fakeSource{name: providerB, rates: map[string]float64{DirectionOffRamp: 123.4}}
	router := newRouter(t, false, yc, fb)

	got, err := router.Best(context.Background(), kesRequest(DirectionOffRamp))
	require.NoError(t, err)
	assert.Equal(t, providerA, got.Provider)
	assert.Equal(t, int32(1), yc.calls.Load())
	assert.Zero(t, fb.calls.Load(), "a disabled relay must not quote other providers")
	assert.False(t, router.Enabled())
}

// One provider being down must not take routing with it.
func TestBest_ToleratesOneFailure(t *testing.T) {
	yc := &fakeSource{name: providerA, rates: map[string]float64{DirectionOffRamp: 120}}
	fb := &fakeSource{name: providerB, err: errors.New("provider down")}
	router := newRouter(t, true, yc, fb)

	got, err := router.Best(context.Background(), kesRequest(DirectionOffRamp))
	require.NoError(t, err)
	assert.Equal(t, providerA, got.Provider)
}

// A provider source is third-party-facing code running on our goroutine; a
// panic in one must not take the process down.
func TestBest_RecoversFromAPanickingSource(t *testing.T) {
	yc := &fakeSource{name: providerA, rates: map[string]float64{DirectionOffRamp: 120}}
	router := newRouter(t, true, yc, panicSource{})

	got, err := router.Best(context.Background(), kesRequest(DirectionOffRamp))
	require.NoError(t, err)
	assert.Equal(t, providerA, got.Provider)
}

func TestBest_PanicIsReportedWhenNothingElseSucceeds(t *testing.T) {
	source := panicSource{}
	router, err := NewWithSources(Config{
		Enabled: true,
		Default: source.Name(),
		Logger:  quietLogger(),
	}, source)
	require.NoError(t, err)

	_, err = router.Best(context.Background(), kesRequest(DirectionOffRamp))
	require.Error(t, err)
	// oops resolves Code from the innermost error that set one, so the panic
	// survives the outer rate_unavailable — which is what an on-call engineer
	// needs to see.
	assert.Equal(t, pkgErrors.CodePanicRecovered, codeOf(t, err))
	assert.Contains(t, err.Error(), "no provider could price this corridor")
}

type panicSource struct{}

func (panicSource) Name() string { return "panicky" }

func (panicSource) QuoteRate(context.Context, RateRequest) (*RateQuote, error) {
	panic("provider blew up")
}

func TestBest_AllFailuresIsAnError(t *testing.T) {
	yc := &fakeSource{name: providerA, err: errors.New("down")}
	fb := &fakeSource{name: providerB, err: errors.New("down")}
	router := newRouter(t, true, yc, fb)

	_, err := router.Best(context.Background(), kesRequest(DirectionOffRamp))
	require.Error(t, err)
	assert.Equal(t, pkgErrors.CodeRateUnavailable, codeOf(t, err))
}

// A source answering with a zero rate is unusable, not a winner.
func TestBest_IgnoresNonPositiveRates(t *testing.T) {
	yc := &fakeSource{name: providerA, rates: map[string]float64{DirectionOnRamp: 0}}
	fb := &fakeSource{name: providerB, rates: map[string]float64{DirectionOnRamp: 132.65}}
	router := newRouter(t, true, yc, fb)

	got, err := router.Best(context.Background(), kesRequest(DirectionOnRamp))
	require.NoError(t, err)
	assert.Equal(t, providerB, got.Provider, "a zero rate must not win an on-ramp by being smallest")
}

func TestBest_ValidatesRequest(t *testing.T) {
	router := newRouter(t, true, &fakeSource{name: providerA, rates: map[string]float64{DirectionOffRamp: 120}})

	tests := map[string]RateRequest{
		"bad direction": {Direction: "sideways", FiatCurrency: "KES", CryptoAmount: 20},
		"no currency":   {Direction: DirectionOffRamp, CryptoAmount: 20},
		"zero amount":   {Direction: DirectionOffRamp, FiatCurrency: "KES"},
		"negative":      {Direction: DirectionOffRamp, FiatCurrency: "KES", CryptoAmount: -1},
	}
	for name, req := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := router.Best(context.Background(), req)
			require.Error(t, err)
		})
	}
}

// The observed sandbox figures: buying costs 132.65, selling yields 123.40.
func TestSpread(t *testing.T) {
	fb := &fakeSource{name: providerB, rates: map[string]float64{
		DirectionOffRamp: 123.4, DirectionOnRamp: 132.65,
	}}
	yc := &fakeSource{name: providerA, rates: map[string]float64{
		DirectionOffRamp: 120, DirectionOnRamp: 135,
	}}
	router := newRouter(t, true, yc, fb)

	spread, err := router.Spread(context.Background(), kesRequest(DirectionOffRamp))
	require.NoError(t, err)

	assert.Equal(t, 132.65, spread.BuyRate)
	assert.Equal(t, 123.4, spread.SellRate)
	assert.Equal(t, providerB, spread.BuyFrom)
	assert.Equal(t, providerB, spread.SellTo)
	assert.InDelta(t, -0.0697, spread.Margin, 0.0001, "the round trip loses 6.97%")
}

func TestBestWithinMargin(t *testing.T) {
	tests := []struct {
		name      string
		sell, buy float64
		floor     float64
		wantErr   bool
	}{
		{"profitable clears a zero floor", 140, 130, 0, false},
		{"loss fails a zero floor", 123.4, 132.65, 0, true},
		{"thin profit fails a 5% floor", 133, 130, 0.05, true},
		{"fat profit clears a 5% floor", 140, 130, 0.05, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := &fakeSource{name: providerA, rates: map[string]float64{
				DirectionOffRamp: tt.sell, DirectionOnRamp: tt.buy,
			}}
			router, err := NewWithSources(Config{
				Enabled:               true,
				Default:               providerA,
				MinRoundTripMarginPct: tt.floor,
				Logger:                quietLogger(),
			}, src)
			require.NoError(t, err)

			got, err := router.BestWithinMargin(context.Background(), kesRequest(DirectionOffRamp))
			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, pkgErrors.CodeMarginTooLow, codeOf(t, err))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.sell, got.EffectiveRate)
		})
	}
}

func TestNew_Validation(t *testing.T) {
	ok := &fakeSource{name: providerA}

	tests := map[string]struct {
		cfg     Config
		sources []RateSource
	}{
		"no sources":      {Config{Default: providerA}, nil},
		"no default":      {Config{}, []RateSource{ok}},
		"unknown default": {Config{Default: "nobody"}, []RateSource{ok}},
		"unnamed source":  {Config{Default: providerA}, []RateSource{&fakeSource{}}},
		"duplicate":       {Config{Default: providerA}, []RateSource{ok, ok}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := NewWithSources(tt.cfg, tt.sources...)
			require.Error(t, err)
		})
	}

	t.Run("nil registry", func(t *testing.T) {
		_, err := New(Config{Default: providerA})
		require.Error(t, err)
	})
}

func TestRegistry(t *testing.T) {
	a := &fakeSource{name: providerA}
	b := &fakeSource{name: providerB}

	registry := NewRegistry()
	require.NoError(t, registry.Register(a))
	require.NoError(t, registry.Register(b))

	assert.Equal(t, 2, registry.Len())
	assert.Equal(t, []string{providerA, providerB}, registry.Names())

	got, ok := registry.Get(providerB)
	require.True(t, ok)
	assert.Same(t, b, got)

	_, ok = registry.Get("nobody")
	assert.False(t, ok)

	require.Error(t, registry.Register(a), "duplicate")
	require.Error(t, registry.Register(nil), "nil source")
	require.Error(t, registry.Register(&fakeSource{}), "unnamed source")

	// All hands out a copy; mutating it must not reorder the registry.
	all := registry.All()
	all[0] = nil
	assert.Equal(t, []string{providerA, providerB}, registry.Names())
}

// A one-way rail declares what it serves so the Router skips it rather than
// logging a failure per transaction.
type offRampOnlySource struct{ *fakeSource }

func (offRampOnlySource) SupportsDirection(direction string) bool {
	return direction == DirectionOffRamp
}

func TestDirectionalSource(t *testing.T) {
	oneWay := offRampOnlySource{&fakeSource{name: providerB, rates: map[string]float64{
		DirectionOffRamp: 140, DirectionOnRamp: 1,
	}}}
	both := &fakeSource{name: providerA, rates: map[string]float64{
		DirectionOffRamp: 120, DirectionOnRamp: 130,
	}}
	router := newRouter(t, true, both, oneWay)

	sell, err := router.Best(context.Background(), kesRequest(DirectionOffRamp))
	require.NoError(t, err)
	assert.Equal(t, providerB, sell.Provider)

	buy, err := router.Best(context.Background(), kesRequest(DirectionOnRamp))
	require.NoError(t, err)
	assert.Equal(t, providerA, buy.Provider, "a one-way source must not win the direction it cannot serve")
	assert.Equal(t, int32(1), oneWay.calls.Load(), "and must not be called for it")
}

func TestBest_NoSourceServesTheDirection(t *testing.T) {
	oneWay := offRampOnlySource{&fakeSource{name: providerA, rates: map[string]float64{DirectionOffRamp: 140}}}
	router := newRouter(t, true, oneWay)

	_, err := router.Best(context.Background(), kesRequest(DirectionOnRamp))
	require.Error(t, err)
	assert.Equal(t, pkgErrors.CodeCorridorUnavailable, codeOf(t, err))
}

func TestRateQuote_Better(t *testing.T) {
	offHigh := RateQuote{Direction: DirectionOffRamp, EffectiveRate: 123}
	offLow := RateQuote{Direction: DirectionOffRamp, EffectiveRate: 120}
	assert.True(t, offHigh.Better(offLow))
	assert.False(t, offLow.Better(offHigh))

	onLow := RateQuote{Direction: DirectionOnRamp, EffectiveRate: 130}
	onHigh := RateQuote{Direction: DirectionOnRamp, EffectiveRate: 133}
	assert.True(t, onLow.Better(onHigh))
	assert.False(t, onHigh.Better(onLow))
}
