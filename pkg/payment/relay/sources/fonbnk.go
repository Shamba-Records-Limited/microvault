package sources

import (
	"context"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/fonbnk"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/relay"
)

// FonbnkQuoter is the slice of the Fonbnk client this source needs.
type FonbnkQuoter interface {
	QuoteOffRamp(ctx context.Context, crypto fonbnk.CryptoLeg, fiat fonbnk.FiatLeg, cryptoAmount float64) (*fonbnk.Quote, error)
	QuoteOnRamp(ctx context.Context, fiat fonbnk.FiatLeg, crypto fonbnk.CryptoLeg, cryptoAmount float64) (*fonbnk.Quote, error)
}

// FonbnkSource prices a corridor with Fonbnk.
type FonbnkSource struct {
	client       FonbnkQuoter
	cryptoCode   string
	carrierCodes map[string]string
	channel      string
	transferType string
}

// FonbnkSourceConfig wires a FonbnkSource. CryptoCurrencyCode is Fonbnk's own
// code, e.g. STELLAR_USDC. CarrierCodes maps fiat currency to carrier.
type FonbnkSourceConfig struct {
	Client              FonbnkQuoter
	CryptoCurrencyCode  string
	CarrierCodes        map[string]string
	PaymentChannel      string
	DepositTransferType string
}

var _ relay.RateSource = (*FonbnkSource)(nil)

// NewFonbnkSource validates the config and returns a source.
func NewFonbnkSource(cfg FonbnkSourceConfig) (*FonbnkSource, error) {
	errb := relay.SourceErr("new_fonbnk_source").With(pkgErrors.AttrProvider, fonbnk.ProviderName)

	if cfg.Client == nil {
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("fonbnk client is nil")
	}
	if cfg.CryptoCurrencyCode == "" {
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("crypto currency code is required")
	}

	channel := cfg.PaymentChannel
	if channel == "" {
		channel = fonbnk.ChannelMobileMoney
	}
	return &FonbnkSource{
		client:       cfg.Client,
		cryptoCode:   cfg.CryptoCurrencyCode,
		carrierCodes: cfg.CarrierCodes,
		channel:      channel,
		transferType: cfg.DepositTransferType,
	}, nil
}

// Name identifies this source.
func (s *FonbnkSource) Name() string { return fonbnk.ProviderName }

// QuoteRate prices the corridor at the requested crypto amount.
func (s *FonbnkSource) QuoteRate(ctx context.Context, req relay.RateRequest) (*relay.RateQuote, error) {
	errb := relay.SourceErr("quote_rate").
		With(pkgErrors.AttrProvider, s.Name()).
		With(pkgErrors.AttrDirection, req.Direction).
		With(pkgErrors.AttrCurrency, req.FiatCurrency)

	crypto := fonbnk.CryptoLeg{CurrencyCode: s.cryptoCode}
	fiat := fonbnk.FiatLeg{
		CurrencyCode:   req.FiatCurrency,
		CountryIsoCode: req.CountryCode,
		PaymentChannel: s.channel,
		CarrierCode:    s.carrierCodes[req.FiatCurrency],
		TransferType:   s.transferType,
	}

	quote, err := s.quote(ctx, req, crypto, fiat)
	if err != nil {
		return nil, err
	}

	rate, ok := quote.EffectiveRate()
	if !ok {
		return nil, errb.Code(pkgErrors.CodeRateUnavailable).
			With(pkgErrors.AttrQuoteID, quote.QuoteID).
			Errorf("quote carries no usable effective rate")
	}

	return &relay.RateQuote{
		Provider:      s.Name(),
		Direction:     req.Direction,
		FiatCurrency:  req.FiatCurrency,
		CryptoAmount:  cryptoAmountOf(quote, req.Direction),
		FiatAmount:    fiatAmountOf(quote, req.Direction),
		EffectiveRate: rate,
		Payload:       quote,
	}, nil
}

// quote runs the direction-appropriate call. Both put the amount on the crypto
// leg, so one call prices the corridor at the size actually being moved.
func (s *FonbnkSource) quote(ctx context.Context, req relay.RateRequest, crypto fonbnk.CryptoLeg, fiat fonbnk.FiatLeg) (*fonbnk.Quote, error) {
	if req.Direction == relay.DirectionOnRamp {
		return s.client.QuoteOnRamp(ctx, fiat, crypto, req.CryptoAmount)
	}
	return s.client.QuoteOffRamp(ctx, crypto, fiat, req.CryptoAmount)
}

func cryptoAmountOf(q *fonbnk.Quote, direction string) float64 {
	if direction == relay.DirectionOnRamp {
		return q.Payout.Cashout.AmountAfterFees
	}
	return q.Deposit.Cashout.AmountBeforeFees
}

func fiatAmountOf(q *fonbnk.Quote, direction string) float64 {
	if direction == relay.DirectionOnRamp {
		return q.Deposit.Cashout.AmountBeforeFees
	}
	return q.Payout.Cashout.AmountAfterFees
}
