package sources

import (
	"context"

	"github.com/samber/lo"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/relay"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/yellowcard"
)

// YellowCardProviderName identifies this source in routing decisions.
const YellowCardProviderName = "yellowcard"

// YellowCardQuoter is the slice of the YellowCard client this source needs.
type YellowCardQuoter interface {
	GetRates(ctx context.Context, currency string) ([]yellowcard.Rate, error)
	GetChannels(ctx context.Context, country string) ([]yellowcard.Channel, error)
}

// YellowCardSource prices a corridor with YellowCard.
//
// YellowCard publishes a headline rate and a per-channel fee rather than a
// priced quote, so the effective rate is computed here.
type YellowCardSource struct {
	client      YellowCardQuoter
	channelType string
}

// YellowCardSourceConfig wires a YellowCardSource.
type YellowCardSourceConfig struct {
	Client      YellowCardQuoter
	ChannelType string
}

var _ relay.RateSource = (*YellowCardSource)(nil)

// NewYellowCardSource validates the config and returns a source.
func NewYellowCardSource(cfg YellowCardSourceConfig) (*YellowCardSource, error) {
	if cfg.Client == nil {
		return nil, relay.SourceErr("new_yellowcard_source").
			With(pkgErrors.AttrProvider, YellowCardProviderName).
			Code(pkgErrors.CodeMissingDependency).
			Errorf("yellowcard client is nil")
	}
	channelType := cfg.ChannelType
	if channelType == "" {
		channelType = yellowcard.ChannelTypeMomo
	}
	return &YellowCardSource{client: cfg.Client, channelType: channelType}, nil
}

// Name identifies this source.
func (s *YellowCardSource) Name() string { return YellowCardProviderName }

// QuoteRate prices the corridor at the requested crypto amount.
func (s *YellowCardSource) QuoteRate(ctx context.Context, req relay.RateRequest) (*relay.RateQuote, error) {
	errb := relay.SourceErr("quote_rate").
		With(pkgErrors.AttrProvider, s.Name()).
		With(pkgErrors.AttrDirection, req.Direction).
		With(pkgErrors.AttrCurrency, req.FiatCurrency)

	rates, err := s.client.GetRates(ctx, req.FiatCurrency)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeRateUnavailable).Wrapf(err, "could not read rates")
	}
	rate, ok := lo.Find(rates, func(r yellowcard.Rate) bool { return r.Code == req.FiatCurrency })
	if !ok {
		return nil, errb.Code(pkgErrors.CodeRateUnavailable).Errorf("no rate published for this currency")
	}

	channel, err := s.channel(ctx, req)
	if err != nil {
		return nil, err
	}

	headline := rate.Sell
	if req.Direction == relay.DirectionOnRamp {
		headline = rate.Buy
	}
	if headline <= 0 {
		return nil, errb.Code(pkgErrors.CodeRateUnavailable).
			With("rate_id", rate.RateID).
			Errorf("published rate is not positive")
	}

	fiat, effective, ok := effectiveYCRate(req, headline, channel)
	if !ok {
		return nil, errb.Code(pkgErrors.CodeRateUnavailable).
			With("fee_usd", channel.FeeUSD).
			With("fee_local", channel.FeeLocal).
			Errorf("fees exceed the amount being moved")
	}

	return &relay.RateQuote{
		Provider:      s.Name(),
		Direction:     req.Direction,
		FiatCurrency:  req.FiatCurrency,
		CryptoAmount:  req.CryptoAmount,
		FiatAmount:    fiat,
		EffectiveRate: effective,
		Payload:       channel,
	}, nil
}

// effectiveYCRate applies the channel's flat fees to the headline rate.
//
// Fees are charged in both currencies, so an off-ramp loses FeeUSD before
// conversion and FeeLocal after it; an on-ramp pays both on top.
func effectiveYCRate(req relay.RateRequest, headline float64, channel yellowcard.Channel) (fiat, effective float64, ok bool) {
	if req.Direction == relay.DirectionOnRamp {
		fiat = (req.CryptoAmount+channel.FeeUSD)*headline + channel.FeeLocal
	} else {
		fiat = (req.CryptoAmount-channel.FeeUSD)*headline - channel.FeeLocal
	}
	if fiat <= 0 {
		return 0, 0, false
	}
	return fiat, fiat / req.CryptoAmount, true
}

// channel picks the active channel serving this direction and currency.
//
// A channel serves one direction, so an off-ramp must not be priced against a
// deposit channel's fees.
func (s *YellowCardSource) channel(ctx context.Context, req relay.RateRequest) (yellowcard.Channel, error) {
	errb := relay.SourceErr("select_channel").
		With(pkgErrors.AttrProvider, s.Name()).
		With(pkgErrors.AttrDirection, req.Direction).
		With(pkgErrors.AttrCurrency, req.FiatCurrency)

	channels, err := s.client.GetChannels(ctx, req.CountryCode)
	if err != nil {
		return yellowcard.Channel{}, errb.Code(pkgErrors.CodeRateUnavailable).Wrapf(err, "could not read channels")
	}

	rampType := yellowcard.RampTypeWithdraw
	if req.Direction == relay.DirectionOnRamp {
		rampType = yellowcard.RampTypeDeposit
	}

	active := yellowcard.FilterActiveChannels(channels, s.channelType)
	matching := lo.Filter(yellowcard.FilterChannelsByRampType(active, rampType), func(c yellowcard.Channel, _ int) bool {
		return c.Currency == req.FiatCurrency
	})
	if len(matching) == 0 {
		return yellowcard.Channel{}, errb.
			With("ramp_type", rampType).
			With("channel_type", s.channelType).
			Code(pkgErrors.CodeCorridorUnavailable).
			Errorf("no active channel serves this corridor")
	}

	// Cheapest first: the fees are what separate two channels on one corridor.
	return lo.Reduce(matching[1:], func(best yellowcard.Channel, c yellowcard.Channel, _ int) yellowcard.Channel {
		if c.FeeUSD < best.FeeUSD {
			return c
		}
		return best
	}, matching[0]), nil
}
