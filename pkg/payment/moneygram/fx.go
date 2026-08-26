package moneygram

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/offramp"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/stellaranchor"
)

// Source labels for the rate ultimately returned by the FX orchestrator.
// These exact strings are persisted on `loans.entry_rate_source` for audit
// Do not rename without a migration.
const (
	RateSourceMoneyGram     = "moneygram_fx_rate"
	RateSourceFallback      = "yellowcard_fallback"
	RateSourceCachedPrimary = "cached_primary"
	RateSourceCachedFall    = "cached_fallback"
)

// Default entry buffers, applied when FXOrchestratorConfig leaves them unset.
const (
	DefaultEntryBufferPct         = 0.01
	DefaultEntryBufferPctFallback = 0.015
)

// ErrAmountOutOfRange is returned by FXOrchestrator.ValidateAmount when the
// USD amount falls outside the configured corridor min/max.
var ErrAmountOutOfRange = errors.New("moneygram: amount out of corridor range")

// ErrNoRateAvailable is returned by Quote when MG primary, the fallback, and
// the stale cache are all unusable. Callers must reject the loan request.
var ErrNoRateAvailable = errors.New("moneygram: no FX rate available from any source")

// FallbackRateSource is implemented by anything that can return a USD to
// local-currency exchange rate when MoneyGram's REST FX endpoint is
// unavailable. The microvault integration wires `yellowcard.YellowcardAdapter`
// behind a thin function adapter (FallbackRateFunc); tests can supply fakes.
type FallbackRateSource interface {
	Get(ctx context.Context, currency string) (rate float64, err error)
}

// FallbackRateFunc adapts a free function to FallbackRateSource. Useful for
// wiring an existing rate provider (e.g. yellowcard.YellowcardAdapter.GetRates)
// without defining a wrapper struct.
type FallbackRateFunc func(ctx context.Context, currency string) (float64, error)

// Get satisfies FallbackRateSource.
func (f FallbackRateFunc) Get(ctx context.Context, currency string) (float64, error) {
	return f(ctx, currency)
}

// FXOrchestratorConfig configures the rate-source cascade.
type FXOrchestratorConfig struct {
	// EntryBufferPct is applied as a deduction to the primary (MG) rate to
	// hedge against drift between USSD entry and webview confirmation.
	// Expressed as a fraction (0.01 = 1 %). Nil defaults to 0.01; an explicit
	// 0 quotes the raw rate.
	//
	// A pointer because zero is a meaningful setting here and has to stay
	// distinguishable from "not configured".
	EntryBufferPct *float64

	// EntryBufferPctFallback is the larger buffer applied to the YC fallback
	// rate, since MG's locked rate may diverge further from YC's. Nil defaults
	// to 0.015; an explicit 0 quotes the raw rate.
	EntryBufferPctFallback *float64

	// StaleCacheMaxAge bounds how old a cached rate may be before the
	// orchestrator stops accepting it (last resort). Default 24h.
	StaleCacheMaxAge time.Duration

	// MinUSD / MaxUSD bound the corridor amount. Defaults: 5 / 3000.
	MinUSD, MaxUSD float64
}

// FXQuoteRequest selects a corridor for the orchestrator.
type FXQuoteRequest struct {
	// OriginatingCountry / DestinationCountry — ISO-3 codes (e.g. "USA", "KEN")
	// for the MG primary call.
	OriginatingCountry string
	DestinationCountry string

	// SendCurrency / ReceiveCurrency — ISO-4217 (e.g. "USD", "KES").
	// SendCurrency is what the treasury holds (USDC ≈ USD); ReceiveCurrency
	// is what the user receives at pickup.
	SendCurrency    string
	ReceiveCurrency string
}

// FXQuoteResult is what the orchestrator returns.
type FXQuoteResult struct {
	// Rate is local-per-USD with the appropriate buffer already applied.
	Rate float64

	// Source labels which provider produced the rate. Persist on
	// `loans.entry_rate_source` — see RateSource* constants.
	Source string

	// BufferPct is the buffer that was applied (for audit on
	// `loans.entry_buffer_pct`).
	BufferPct float64

	// FetchedAt is when the rate was originally fetched. For cached results
	// this can be hours old; for fresh results it's effectively `time.Now()`.
	FetchedAt time.Time
}

// FXOrchestrator implements the §7 strategy: try MG primary to fallback to
// stale cache. Last-known-good is held per (sendCurrency, receiveCurrency)
// pair and survives across calls within a single process lifetime.
type FXOrchestrator struct {
	primary  *FXRateClient
	fallback FallbackRateSource
	cfg      FXOrchestratorConfig
	logger   *slog.Logger

	primaryBuffer  offramp.RateBuffer
	fallbackBuffer offramp.RateBuffer

	mu    sync.Mutex
	cache map[string]FXQuoteResult // key = "send:receive"
}

// NewFXOrchestrator constructs an orchestrator. Either primary or fallback
// may be nil — but not both. Callers must treat ErrNoRateAvailable as a
// terminal "reject the loan" condition.
func NewFXOrchestrator(primary *FXRateClient, fallback FallbackRateSource, cfg FXOrchestratorConfig, logger *slog.Logger) (*FXOrchestrator, error) {
	if primary == nil && fallback == nil {
		return nil, fxErr().Code(pkgErrors.CodeMissingDependency).
			Wrapf(stellaranchor.ErrInvalidConfig, "FX orchestrator needs at least one rate source")
	}
	if cfg.StaleCacheMaxAge == 0 {
		cfg.StaleCacheMaxAge = 24 * time.Hour
	}
	if cfg.MinUSD == 0 {
		cfg.MinUSD = 5
	}
	if cfg.MaxUSD == 0 {
		cfg.MaxUSD = 3000
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &FXOrchestrator{
		primary:        primary,
		fallback:       fallback,
		cfg:            cfg,
		primaryBuffer:  offramp.NewRateBuffer(cfg.EntryBufferPct, DefaultEntryBufferPct),
		fallbackBuffer: offramp.NewRateBuffer(cfg.EntryBufferPctFallback, DefaultEntryBufferPctFallback),
		logger:         logger.With("component", "moneygram_fx_orchestrator"),
		cache:          make(map[string]FXQuoteResult),
	}, nil
}

// Quote returns a buffered exchange rate. Strategy:
//  1. Try MG primary with EntryBufferPct.
//  2. On primary failure, try the fallback with EntryBufferPctFallback.
//  3. On both failures, return the most recent cached value if its FetchedAt
//     is within StaleCacheMaxAge, with the original source preserved.
//  4. Else return ErrNoRateAvailable.
func (o *FXOrchestrator) Quote(ctx context.Context, req FXQuoteRequest) (*FXQuoteResult, error) {
	if req.SendCurrency == "" || req.ReceiveCurrency == "" {
		return nil, fxErr().Code(pkgErrors.CodeMissingAccount).
			Wrapf(stellaranchor.ErrInvalidConfig, "FX quote needs a send and receive currency")
	}

	// 1) Primary — MoneyGram.
	if o.primary != nil && req.OriginatingCountry != "" && req.DestinationCountry != "" {
		fx, err := o.primary.Get(ctx, FXRateRequest{
			OriginatingCountry: req.OriginatingCountry,
			SendCurrency:       req.SendCurrency,
			DestinationCountry: req.DestinationCountry,
			ServiceOption:      ServiceOptionCashPickup,
		})
		if err == nil {
			res := &FXQuoteResult{
				Rate:      o.primaryBuffer.Apply(fx.Rate),
				Source:    RateSourceMoneyGram,
				BufferPct: o.primaryBuffer.Pct(),
				FetchedAt: fx.FetchedAt,
			}
			o.cachePut(req, *res)
			return res, nil
		}
		o.logger.Warn("primary rate source failed, attempting fallback",
			"send", req.SendCurrency, "receive", req.ReceiveCurrency, "error", err)
	}

	// 2) Fallback — typically YellowCard.
	if o.fallback != nil {
		raw, err := o.fallback.Get(ctx, req.ReceiveCurrency)
		if err == nil && raw > 0 {
			res := &FXQuoteResult{
				Rate:      o.fallbackBuffer.Apply(raw),
				Source:    RateSourceFallback,
				BufferPct: o.fallbackBuffer.Pct(),
				FetchedAt: time.Now(),
			}
			o.cachePut(req, *res)
			return res, nil
		}
		if err != nil {
			o.logger.Error("fallback rate source failed",
				"receive", req.ReceiveCurrency, "error", err)
		}
	}

	// 3) Stale cache — last resort, preserve original source label but mark cached.
	if cached, ok := o.cacheGet(req); ok {
		age := time.Since(cached.FetchedAt)
		if age <= o.cfg.StaleCacheMaxAge {
			cached.Source = stalenessLabel(cached.Source)
			o.logger.Warn("serving stale cached FX rate",
				"send", req.SendCurrency, "receive", req.ReceiveCurrency,
				"source", cached.Source, "age", age)
			return &cached, nil
		}
		o.logger.Warn("cached FX rate too old, refusing to serve",
			"send", req.SendCurrency, "receive", req.ReceiveCurrency, "age", age)
	}

	return nil, ErrNoRateAvailable
}

// EntryBufferPct reports the buffer applied to the primary leg, after
// defaulting. Zero means rates are quoted raw.
func (o *FXOrchestrator) EntryBufferPct() float64 { return o.primaryBuffer.Pct() }

// EntryBufferPctFallback reports the buffer applied to the fallback leg, after
// defaulting. Zero means rates are quoted raw.
func (o *FXOrchestrator) EntryBufferPctFallback() float64 { return o.fallbackBuffer.Pct() }

// ValidateAmount enforces the configured min/max USD caps. Caps come from
// MG's documented corridor limits (typically $5 floor, $1000–$3000 ceiling
// depending on country) — set them explicitly per environment.
func (o *FXOrchestrator) ValidateAmount(amountUSD float64) error {
	if amountUSD < o.cfg.MinUSD {
		return fxErr().
			With("amount_usd", amountUSD).
			With("min_usd", o.cfg.MinUSD).
			Code(pkgErrors.CodeBelowAnchorMinimum).
			Wrapf(ErrAmountOutOfRange, "amount is below the corridor minimum")
	}
	if amountUSD > o.cfg.MaxUSD {
		return fxErr().
			With("amount_usd", amountUSD).
			With("max_usd", o.cfg.MaxUSD).
			Code(pkgErrors.CodeInvalidAmount).
			Wrapf(ErrAmountOutOfRange, "amount is above the corridor maximum")
	}
	return nil
}

func (o *FXOrchestrator) cacheKey(req FXQuoteRequest) string {
	return req.SendCurrency + ":" + req.ReceiveCurrency
}

func (o *FXOrchestrator) cachePut(req FXQuoteRequest, res FXQuoteResult) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.cache[o.cacheKey(req)] = res
}

func (o *FXOrchestrator) cacheGet(req FXQuoteRequest) (FXQuoteResult, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	v, ok := o.cache[o.cacheKey(req)]
	return v, ok
}

// stalenessLabel maps a fresh source label to its cached counterpart.
// Unknown labels pass through unchanged.
func stalenessLabel(source string) string {
	switch source {
	case RateSourceMoneyGram:
		return RateSourceCachedPrimary
	case RateSourceFallback:
		return RateSourceCachedFall
	}
	return source
}
