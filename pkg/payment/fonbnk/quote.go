package fonbnk

import (
	"context"
	"net/http"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// CreateQuote locks a rate and reports which fields an order from it must
// carry. Set an amount on exactly one leg.
func (a *FonbnkAdapter) CreateQuote(ctx context.Context, req QuoteRequest) (*Quote, error) {
	errb := withDirection(
		fonbnkErr("create_quote").
			With("deposit_currency", req.Deposit.CurrencyCode).
			With("payout_currency", req.Payout.CurrencyCode),
		req.Deposit.CurrencyType, req.Payout.CurrencyType)

	if err := exactlyOneAmount(errb, req.Deposit.Amount, req.Payout.Amount); err != nil {
		return nil, err
	}
	return call[Quote](ctx, a, errb, http.MethodPost, pathQuote, req)
}

// QuoteOffRamp prices selling cryptoAmount of crypto for fiat paid out over a
// payment channel.
//
// The amount goes on the crypto leg in both directions, because that is the
// side we always know: a loan is denominated in USDC, not in shillings.
func (a *FonbnkAdapter) QuoteOffRamp(ctx context.Context, crypto CryptoLeg, fiat FiatLeg, cryptoAmount float64) (*Quote, error) {
	return a.CreateQuote(ctx, QuoteRequest{
		Deposit: QuoteDepositLeg{LegSpec: crypto.depositSpec(&cryptoAmount)},
		Payout:  fiat.spec(nil),
	})
}

// QuoteOnRamp prices buying cryptoAmount of crypto with fiat collected over a
// payment channel. See QuoteOffRamp on which leg carries the amount.
func (a *FonbnkAdapter) QuoteOnRamp(ctx context.Context, fiat FiatLeg, crypto CryptoLeg, cryptoAmount float64) (*Quote, error) {
	return a.CreateQuote(ctx, QuoteRequest{
		Deposit: QuoteDepositLeg{
			LegSpec:      fiat.spec(nil),
			TransferType: fiat.TransferType,
		},
		Payout: crypto.payoutSpec(&cryptoAmount),
	})
}

// CryptoLeg names a crypto side of a corridor, e.g. STELLAR_USDC.
type CryptoLeg struct {
	CurrencyCode string
}

func (c CryptoLeg) depositSpec(amount *float64) LegSpec {
	return LegSpec{
		PaymentChannel: ChannelCrypto,
		CurrencyType:   CurrencyTypeCrypto,
		CurrencyCode:   c.CurrencyCode,
		Amount:         amount,
	}
}

func (c CryptoLeg) payoutSpec(amount *float64) LegSpec { return c.depositSpec(amount) }

// FiatLeg names a fiat side of a corridor. TransferType is only honoured on a
// quote's deposit leg.
type FiatLeg struct {
	CurrencyCode   string
	CountryIsoCode string
	PaymentChannel string
	CarrierCode    string
	TransferType   string
}

func (f FiatLeg) spec(amount *float64) LegSpec {
	channel := f.PaymentChannel
	if channel == "" {
		channel = ChannelMobileMoney
	}
	return LegSpec{
		PaymentChannel: channel,
		CurrencyType:   CurrencyTypeFiat,
		CurrencyCode:   f.CurrencyCode,
		CountryIsoCode: f.CountryIsoCode,
		CarrierCode:    f.CarrierCode,
		Amount:         amount,
	}
}

// quoteExpiredErr reports a quote used past its window.
func quoteExpiredErr(quoteID string) error {
	return fonbnkErr("use_quote").
		With(pkgErrors.AttrQuoteID, quoteID).
		Code(pkgErrors.CodeQuoteExpired).
		Errorf("quote has expired")
}
