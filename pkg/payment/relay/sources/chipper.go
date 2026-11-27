package sources

import (
	"context"
	"strings"

	"github.com/govalues/decimal"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/chipper"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/relay"
)

const chipperAsset = "USDC"

// ChipperRater is the slice of the Chipper client this source needs.
type ChipperRater interface {
	GetRate(ctx context.Context, origin, destination string) (*chipper.Rate, error)
}

// ChipperSource prices an off-ramp with Chipper: the all-in USDC rate, less
// the scheduled deposit and payout fees.
type ChipperSource struct {
	client      ChipperRater
	fees        chipper.FeeSchedule
	methodCodes map[string]string
}

// ChipperSourceConfig wires a ChipperSource. MethodCodes maps a fiat
// currency to the payout method whose fee rule applies, e.g. KES → ke_mpesa.
type ChipperSourceConfig struct {
	Client      ChipperRater
	Fees        *chipper.FeeSchedule
	MethodCodes map[string]string
}

var (
	_ relay.RateSource        = (*ChipperSource)(nil)
	_ relay.DirectionalSource = (*ChipperSource)(nil)
)

// NewChipperSource validates the config. Every mapped method must have a fee
// rule: an unpriced quote would win routing for free.
func NewChipperSource(cfg ChipperSourceConfig) (*ChipperSource, error) {
	errb := relay.SourceErr("new_chipper_source").With(pkgErrors.AttrProvider, chipper.ProviderName)

	switch {
	case cfg.Client == nil:
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("chipper client is nil")
	case cfg.Fees == nil:
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("chipper fee schedule is required")
	case len(cfg.MethodCodes) == 0:
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("no payout method codes configured")
	}
	codes := make(map[string]string, len(cfg.MethodCodes))
	for currency, code := range cfg.MethodCodes {
		if _, ok := cfg.Fees.PayoutRule(code); !ok {
			return nil, errb.Code(pkgErrors.CodeInvalidConfig).With("method_code", code).
				Errorf("payout method has no fee rule")
		}
		codes[strings.ToUpper(currency)] = code
	}
	return &ChipperSource{client: cfg.Client, fees: *cfg.Fees, methodCodes: codes}, nil
}

// Name identifies this source.
func (s *ChipperSource) Name() string { return chipper.ProviderName }

// SupportsDirection reports that only off-ramps are priced.
func (s *ChipperSource) SupportsDirection(direction string) bool {
	return direction == relay.DirectionOffRamp
}

// QuoteRate prices the off-ramp at the requested USDC amount.
func (s *ChipperSource) QuoteRate(ctx context.Context, req relay.RateRequest) (*relay.RateQuote, error) {
	errb := relay.SourceErr("quote_rate").
		With(pkgErrors.AttrProvider, s.Name()).
		With(pkgErrors.AttrDirection, req.Direction).
		With(pkgErrors.AttrCurrency, req.FiatCurrency)

	if req.Direction != relay.DirectionOffRamp {
		return nil, errb.Code(pkgErrors.CodeCorridorUnavailable).Errorf("chipper prices off-ramps only")
	}
	code, ok := s.methodCodes[strings.ToUpper(req.FiatCurrency)]
	if !ok {
		return nil, errb.Code(pkgErrors.CodeCorridorUnavailable).Errorf("no chipper payout method for this currency")
	}
	rule, _ := s.fees.PayoutRule(code)

	amount, err := decimal.NewFromFloat64(req.CryptoAmount)
	if err != nil || !amount.IsPos() {
		return nil, errb.Code(pkgErrors.CodeInvalidAmount).With("amount", req.CryptoAmount).Errorf("crypto amount is not usable")
	}

	rate, err := s.client.GetRate(ctx, chipperAsset, req.FiatCurrency)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeRateUnavailable).Wrapf(err, "could not read the chipper rate")
	}

	fiat, err := chipperFiat(amount, rate.Rate, s.fees.Deposit, rule)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeRateUnavailable).Wrapf(err, "could not price the corridor")
	}
	if !fiat.IsPos() {
		return nil, errb.Code(pkgErrors.CodeRateUnavailable).Errorf("fees exceed the amount being moved")
	}

	fiatAmount, _ := fiat.Float64()
	return &relay.RateQuote{
		Provider:      s.Name(),
		Direction:     req.Direction,
		FiatCurrency:  req.FiatCurrency,
		CryptoAmount:  req.CryptoAmount,
		FiatAmount:    fiatAmount,
		EffectiveRate: fiatAmount / req.CryptoAmount,
		Payload:       rate,
	}, nil
}

// chipperFiat is the fiat delivered per the comparison convention shared with
// the other sources: the amount less both legs' fees, at the all-in rate.
func chipperFiat(amount, rate decimal.Decimal, deposit, payout chipper.FeeRule) (decimal.Decimal, error) {
	depositFee, err := deposit.Fee(amount)
	if err != nil {
		return decimal.Zero, err
	}
	payoutFee, err := payout.Fee(amount)
	if err != nil {
		return decimal.Zero, err
	}
	net, err := amount.Sub(depositFee)
	if err != nil {
		return decimal.Zero, err
	}
	if net, err = net.Sub(payoutFee); err != nil {
		return decimal.Zero, err
	}
	return net.Mul(rate)
}
