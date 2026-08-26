package relay

import (
	"context"
	"log/slog"
	"sync"

	"github.com/samber/lo"
	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Directions a corridor can run in.
const (
	DirectionOffRamp = "off_ramp"
	DirectionOnRamp  = "on_ramp"
)

func relayErr(op string) oops.OopsErrorBuilder {
	return oops.In(pkgErrors.DomainPaymentRelay).
		Tags("relay").
		With(pkgErrors.AttrOperation, op)
}

// SourceErr starts an error builder for a RateSource implementation, so a
// builder's own source reports failures in the same domain as the platform's.
func SourceErr(op string) oops.OopsErrorBuilder { return relayErr(op) }

// RateRequest asks every source to price one corridor at one amount.
//
// Amount is always in crypto units. Fees are banded, so a rate is only
// meaningful against the amount actually being moved.
type RateRequest struct {
	Direction    string
	FiatCurrency string
	CountryCode  string
	CryptoAmount float64
}

func (r RateRequest) validate(errb oops.OopsErrorBuilder) error {
	switch {
	case r.Direction != DirectionOffRamp && r.Direction != DirectionOnRamp:
		return errb.Code(pkgErrors.CodeInvalidAmount).
			With(pkgErrors.AttrDirection, r.Direction).
			Errorf("direction must be off_ramp or on_ramp")
	case r.FiatCurrency == "":
		return errb.Code(pkgErrors.CodeMissingAccount).Errorf("fiat currency is required")
	case r.CryptoAmount <= 0:
		return errb.Code(pkgErrors.CodeInvalidAmount).
			With(pkgErrors.AttrAmountLocal, r.CryptoAmount).
			Errorf("crypto amount must be positive")
	}
	return nil
}

// RateQuote is one provider's price for a corridor at a specific amount.
//
// EffectiveRate is fiat per unit of crypto after every fee on both legs:
// maximise it on an off-ramp, minimise it on an on-ramp. It is never a
// provider's headline rate.
type RateQuote struct {
	Provider      string
	Direction     string
	FiatCurrency  string
	CryptoAmount  float64
	FiatAmount    float64
	EffectiveRate float64

	// Payload is the provider's own quote, for a caller that goes on to open
	// an order against it.
	Payload any
}

// Better reports whether this quote beats other for its direction.
func (q RateQuote) Better(other RateQuote) bool {
	if q.Direction == DirectionOnRamp {
		return q.EffectiveRate < other.EffectiveRate
	}
	return q.EffectiveRate > other.EffectiveRate
}

// RateSource prices one corridor with one provider. Implement it to add a
// provider the platform has never heard of.
type RateSource interface {
	Name() string
	QuoteRate(ctx context.Context, req RateRequest) (*RateQuote, error)
}

// DirectionalSource narrows a source to the directions it can actually price.
//
// Optional: a source that does not implement it is asked for every direction.
// Implement it on a one-way rail — MoneyGram cash pickup pays out but cannot
// collect — so the Router skips the call instead of logging a failure per
// transaction.
type DirectionalSource interface {
	RateSource
	SupportsDirection(direction string) bool
}

// Config wires a Router.
type Config struct {
	// Registry holds the sources to quote. Required.
	Registry *Registry

	// Enabled turns routing on. Off sends every request to Default.
	Enabled bool

	// Default is the provider used when routing is off, and the one a caller
	// falls back to when routing is unavailable.
	Default string

	// MinRoundTripMarginPct is the floor BestWithinMargin enforces, as a
	// fraction. Zero means break-even.
	MinRoundTripMarginPct float64

	Logger *slog.Logger
}

// Router picks the provider offering the best effective rate.
type Router struct {
	registry              *Registry
	enabled               bool
	defaultProvider       string
	minRoundTripMarginPct float64
	logger                *slog.Logger
}

// New validates the config and returns a Router.
func New(cfg Config) (*Router, error) {
	errb := relayErr("new")

	if cfg.Registry == nil || cfg.Registry.Len() == 0 {
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("registry holds no rate sources")
	}
	if cfg.Default == "" {
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("no default provider was named")
	}
	if _, ok := cfg.Registry.Get(cfg.Default); !ok {
		return nil, errb.Code(pkgErrors.CodeNotFound).
			With(pkgErrors.AttrProvider, cfg.Default).
			With("registered", cfg.Registry.Names()).
			Errorf("default provider is not a registered rate source")
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Router{
		registry:              cfg.Registry,
		enabled:               cfg.Enabled,
		defaultProvider:       cfg.Default,
		minRoundTripMarginPct: cfg.MinRoundTripMarginPct,
		logger:                logger.With("component", "payment_relay"),
	}, nil
}

// NewWithSources is New over an inline set of sources, for callers that have
// no other use for a Registry.
func NewWithSources(cfg Config, sources ...RateSource) (*Router, error) {
	registry := NewRegistry()
	for _, source := range sources {
		if err := registry.Register(source); err != nil {
			return nil, err
		}
	}
	cfg.Registry = registry
	return New(cfg)
}

// Registry returns the sources this Router quotes.
func (r *Router) Registry() *Registry { return r.registry }

// DefaultProvider names the provider used when routing is off.
func (r *Router) DefaultProvider() string { return r.defaultProvider }

// Enabled reports whether routing is on.
func (r *Router) Enabled() bool { return r.enabled }

// Best returns the winning quote for a corridor.
//
// With routing off it quotes only the default provider, so turning the switch
// off is always safe under load. With routing on a single source failing is
// tolerated; every source failing is not.
func (r *Router) Best(ctx context.Context, req RateRequest) (*RateQuote, error) {
	errb := relayErr("best").
		With(pkgErrors.AttrDirection, req.Direction).
		With(pkgErrors.AttrCurrency, req.FiatCurrency)

	if err := req.validate(errb); err != nil {
		return nil, err
	}

	if !r.enabled {
		source, _ := r.registry.Get(r.defaultProvider)
		return r.quoteOne(ctx, source, req, errb)
	}

	serving := r.registry.serving(req.Direction)
	if len(serving) == 0 {
		return nil, errb.
			Code(pkgErrors.CodeCorridorUnavailable).
			With("registered", r.registry.Names()).
			Errorf("no registered source serves this direction")
	}

	quotes, failures := r.quoteAll(ctx, serving, req)
	if len(quotes) == 0 {
		return nil, errb.
			Code(pkgErrors.CodeRateUnavailable).
			With("source_count", len(serving)).
			Wrapf(failures, "no provider could price this corridor")
	}

	best := lo.Reduce(quotes[1:], func(best RateQuote, q RateQuote, _ int) RateQuote {
		if q.Better(best) {
			return q
		}
		return best
	}, quotes[0])

	r.logger.Info("relay routed",
		pkgErrors.AttrDirection, req.Direction,
		pkgErrors.AttrProvider, best.Provider,
		pkgErrors.AttrCurrency, req.FiatCurrency,
		"crypto_amount", req.CryptoAmount,
		"effective_rate", best.EffectiveRate,
		"quoted_sources", len(quotes))

	return &best, nil
}

// BestWithinMargin is Best plus the round-trip guard.
//
// It quotes the opposite direction as well and refuses when selling would
// yield less than buying costs, by more than MinRoundTripMarginPct. Doubles
// the quote calls, so it belongs on treasury-scale movements rather than on
// every borrower transaction.
func (r *Router) BestWithinMargin(ctx context.Context, req RateRequest) (*RateQuote, error) {
	best, err := r.Best(ctx, req)
	if err != nil {
		return nil, err
	}

	spread, err := r.Spread(ctx, req)
	if err != nil {
		return nil, err
	}
	if spread.Margin < r.minRoundTripMarginPct {
		return nil, relayErr("best_within_margin").
			With(pkgErrors.AttrDirection, req.Direction).
			With(pkgErrors.AttrCurrency, req.FiatCurrency).
			With("buy_rate", spread.BuyRate).
			With("sell_rate", spread.SellRate).
			With("margin", spread.Margin).
			With("min_margin", r.minRoundTripMarginPct).
			Code(pkgErrors.CodeMarginTooLow).
			Errorf("round trip would lose more than the configured floor")
	}
	return best, nil
}

// Spread is the round trip across both directions at one amount.
type Spread struct {
	FiatCurrency string
	CryptoAmount float64

	// BuyRate is the fiat cost of one crypto unit on the best on-ramp;
	// SellRate is the fiat yield of one on the best off-ramp.
	BuyRate  float64
	SellRate float64
	BuyFrom  string
	SellTo   string

	// Margin is (sell - buy) / buy. Negative means a round trip loses money.
	Margin float64
}

// Spread quotes both directions and reports the round trip.
func (r *Router) Spread(ctx context.Context, req RateRequest) (*Spread, error) {
	errb := relayErr("spread").With(pkgErrors.AttrCurrency, req.FiatCurrency)

	buy, err := r.Best(ctx, RateRequest{
		Direction:    DirectionOnRamp,
		FiatCurrency: req.FiatCurrency,
		CountryCode:  req.CountryCode,
		CryptoAmount: req.CryptoAmount,
	})
	if err != nil {
		return nil, err
	}
	sell, err := r.Best(ctx, RateRequest{
		Direction:    DirectionOffRamp,
		FiatCurrency: req.FiatCurrency,
		CountryCode:  req.CountryCode,
		CryptoAmount: req.CryptoAmount,
	})
	if err != nil {
		return nil, err
	}
	if buy.EffectiveRate <= 0 {
		return nil, errb.Code(pkgErrors.CodeRateUnavailable).Errorf("on-ramp rate is not positive")
	}

	return &Spread{
		FiatCurrency: req.FiatCurrency,
		CryptoAmount: req.CryptoAmount,
		BuyRate:      buy.EffectiveRate,
		SellRate:     sell.EffectiveRate,
		BuyFrom:      buy.Provider,
		SellTo:       sell.Provider,
		Margin:       (sell.EffectiveRate - buy.EffectiveRate) / buy.EffectiveRate,
	}, nil
}

// quoteOne prices a single source.
func (r *Router) quoteOne(ctx context.Context, source RateSource, req RateRequest, errb oops.OopsErrorBuilder) (*RateQuote, error) {
	quote, err := source.QuoteRate(ctx, req)
	if err != nil {
		return nil, errb.
			With(pkgErrors.AttrProvider, source.Name()).
			Code(pkgErrors.CodeRateUnavailable).
			Wrapf(err, "provider could not price this corridor")
	}
	return quote, nil
}

// quoteAll prices the given sources concurrently, returning the successes and
// the joined failures. Fan-out is I/O, so it uses goroutines rather than lop.
func (r *Router) quoteAll(ctx context.Context, sources []RateSource, req RateRequest) ([]RateQuote, error) {
	type result struct {
		quote *RateQuote
		err   error
	}

	results := make([]result, len(sources))
	var wg sync.WaitGroup

	for i, source := range sources {
		wg.Add(1)
		go func() {
			defer wg.Done()

			errb := relayErr("quote_source").With(pkgErrors.AttrProvider, source.Name())

			// A panicking provider must not take the process down with it.
			var (
				quote   *RateQuote
				callErr error
			)
			panicErr := errb.
				Code(pkgErrors.CodePanicRecovered).
				Recoverf(func() { quote, callErr = source.QuoteRate(ctx, req) }, "rate source panicked")

			switch {
			case panicErr != nil:
				results[i] = result{err: panicErr}
			case callErr != nil:
				results[i] = result{err: errb.Wrapf(callErr, "rate source failed")}
			default:
				results[i] = result{quote: quote}
			}
		}()
	}
	wg.Wait()

	quotes := make([]RateQuote, 0, len(results))
	failures := make([]error, 0, len(results))
	for _, res := range results {
		switch {
		case res.err != nil:
			failures = append(failures, res.err)
		case res.quote != nil && res.quote.EffectiveRate > 0:
			quotes = append(quotes, *res.quote)
		}
	}

	for _, err := range failures {
		r.logger.Warn("rate source failed, excluded from routing",
			pkgErrors.AttrDirection, req.Direction, "error", err)
	}
	return quotes, oops.Join(failures...)
}
