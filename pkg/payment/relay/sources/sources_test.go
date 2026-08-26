package sources

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/oops"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/fonbnk"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/relay"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/yellowcard"
)

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var oopsErr oops.OopsError
	require.True(t, errors.As(err, &oopsErr), "not an oops error: %v", err)
	code, _ := oopsErr.Code().(string)
	return code
}

func kesRequest(direction string) relay.RateRequest {
	return relay.RateRequest{Direction: direction, FiatCurrency: "KES", CountryCode: "KE", CryptoAmount: 20}
}

type fakeYC struct {
	rates    []yellowcard.Rate
	channels []yellowcard.Channel
	ratesErr error
}

func (f *fakeYC) GetRates(context.Context, string) ([]yellowcard.Rate, error) {
	return f.rates, f.ratesErr
}

func (f *fakeYC) GetChannels(context.Context, string) ([]yellowcard.Channel, error) {
	return f.channels, nil
}

func kesChannel(rampType string, feeUSD, feeLocal float64) yellowcard.Channel {
	return yellowcard.Channel{
		ID: rampType, Currency: "KES", Country: "KE",
		ChannelType: yellowcard.ChannelTypeMomo, RampType: rampType,
		Status: "active", APIStatus: "active",
		FeeUSD: feeUSD, FeeLocal: feeLocal,
	}
}

func ycSource(t *testing.T, client YellowCardQuoter) *YellowCardSource {
	t.Helper()
	s, err := NewYellowCardSource(YellowCardSourceConfig{Client: client})
	require.NoError(t, err)
	return s
}

// sell is USD to local and buy is local to USD, both named from the customer's
// side. Reading one for the other inverts every routing decision without
// failing anything, so it is pinned here.
func TestYellowCardSource_UsesSellForOffRampAndBuyForOnRamp(t *testing.T) {
	client := &fakeYC{
		rates: []yellowcard.Rate{{Code: "KES", Sell: 120, Buy: 130, RateID: "kenyan-shilling"}},
		channels: []yellowcard.Channel{
			kesChannel(yellowcard.RampTypeWithdraw, 0, 0),
			kesChannel(yellowcard.RampTypeDeposit, 0, 0),
		},
	}
	source := ycSource(t, client)

	sell, err := source.QuoteRate(context.Background(), kesRequest(relay.DirectionOffRamp))
	require.NoError(t, err)
	assert.Equal(t, 120.0, sell.EffectiveRate, "an off-ramp reads Sell")
	assert.Equal(t, 2400.0, sell.FiatAmount)

	buy, err := source.QuoteRate(context.Background(), kesRequest(relay.DirectionOnRamp))
	require.NoError(t, err)
	assert.Equal(t, 130.0, buy.EffectiveRate, "an on-ramp reads Buy")
	assert.Equal(t, 2600.0, buy.FiatAmount)
}

// Fees are deducted on the way out and added on the way in.
func TestYellowCardSource_AppliesFeesByDirection(t *testing.T) {
	client := &fakeYC{
		rates: []yellowcard.Rate{{Code: "KES", Sell: 120, Buy: 120}},
		channels: []yellowcard.Channel{
			kesChannel(yellowcard.RampTypeWithdraw, 1, 50),
			kesChannel(yellowcard.RampTypeDeposit, 1, 50),
		},
	}
	source := ycSource(t, client)

	sell, err := source.QuoteRate(context.Background(), kesRequest(relay.DirectionOffRamp))
	require.NoError(t, err)
	// (20 - 1) * 120 - 50
	assert.Equal(t, 2230.0, sell.FiatAmount)
	assert.InDelta(t, 111.5, sell.EffectiveRate, 0.0001)

	buy, err := source.QuoteRate(context.Background(), kesRequest(relay.DirectionOnRamp))
	require.NoError(t, err)
	// (20 + 1) * 120 + 50
	assert.Equal(t, 2570.0, buy.FiatAmount)
	assert.InDelta(t, 128.5, buy.EffectiveRate, 0.0001)

	assert.Less(t, sell.EffectiveRate, buy.EffectiveRate, "fees widen the spread both ways")
}

// A channel serves one direction, so an off-ramp priced against a deposit
// channel's fees would be wrong.
func TestYellowCardSource_RampTypeScopesTheChannel(t *testing.T) {
	client := &fakeYC{
		rates:    []yellowcard.Rate{{Code: "KES", Sell: 120, Buy: 120}},
		channels: []yellowcard.Channel{kesChannel(yellowcard.RampTypeDeposit, 5, 0)},
	}
	source := ycSource(t, client)

	_, err := source.QuoteRate(context.Background(), kesRequest(relay.DirectionOffRamp))
	require.Error(t, err)
	assert.Equal(t, pkgErrors.CodeCorridorUnavailable, codeOf(t, err))

	got, err := source.QuoteRate(context.Background(), kesRequest(relay.DirectionOnRamp))
	require.NoError(t, err)
	assert.Equal(t, 5.0, got.Payload.(yellowcard.Channel).FeeUSD)
}

func TestYellowCardSource_PicksCheapestChannel(t *testing.T) {
	expensive := kesChannel(yellowcard.RampTypeWithdraw, 3, 0)
	cheap := kesChannel(yellowcard.RampTypeWithdraw, 1, 0)
	client := &fakeYC{
		rates:    []yellowcard.Rate{{Code: "KES", Sell: 120, Buy: 120}},
		channels: []yellowcard.Channel{expensive, cheap},
	}

	got, err := ycSource(t, client).QuoteRate(context.Background(), kesRequest(relay.DirectionOffRamp))
	require.NoError(t, err)
	assert.Equal(t, 1.0, got.Payload.(yellowcard.Channel).FeeUSD)
}

func TestYellowCardSource_InactiveChannelIsNotUsed(t *testing.T) {
	inactive := kesChannel(yellowcard.RampTypeWithdraw, 1, 0)
	inactive.APIStatus = "inactive"
	client := &fakeYC{
		rates:    []yellowcard.Rate{{Code: "KES", Sell: 120}},
		channels: []yellowcard.Channel{inactive},
	}

	_, err := ycSource(t, client).QuoteRate(context.Background(), kesRequest(relay.DirectionOffRamp))
	require.Error(t, err)
	assert.Equal(t, pkgErrors.CodeCorridorUnavailable, codeOf(t, err))
}

// A flat fee larger than the amount would otherwise produce a negative rate
// and win an on-ramp for being smallest.
func TestYellowCardSource_FeesExceedingAmount(t *testing.T) {
	client := &fakeYC{
		rates:    []yellowcard.Rate{{Code: "KES", Sell: 120}},
		channels: []yellowcard.Channel{kesChannel(yellowcard.RampTypeWithdraw, 50, 0)},
	}

	_, err := ycSource(t, client).QuoteRate(context.Background(), kesRequest(relay.DirectionOffRamp))
	require.Error(t, err)
	assert.Equal(t, pkgErrors.CodeRateUnavailable, codeOf(t, err))
}

func TestYellowCardSource_RateErrors(t *testing.T) {
	t.Run("client failure", func(t *testing.T) {
		client := &fakeYC{ratesErr: errors.New("down")}
		_, err := ycSource(t, client).QuoteRate(context.Background(), kesRequest(relay.DirectionOffRamp))
		require.Error(t, err)
		assert.Equal(t, pkgErrors.CodeRateUnavailable, codeOf(t, err))
	})

	t.Run("currency not published", func(t *testing.T) {
		client := &fakeYC{rates: []yellowcard.Rate{{Code: "NGN", Sell: 1500}}}
		_, err := ycSource(t, client).QuoteRate(context.Background(), kesRequest(relay.DirectionOffRamp))
		require.Error(t, err)
		assert.Equal(t, pkgErrors.CodeRateUnavailable, codeOf(t, err))
	})

	t.Run("zero rate", func(t *testing.T) {
		client := &fakeYC{
			rates:    []yellowcard.Rate{{Code: "KES", Sell: 0}},
			channels: []yellowcard.Channel{kesChannel(yellowcard.RampTypeWithdraw, 0, 0)},
		}
		_, err := ycSource(t, client).QuoteRate(context.Background(), kesRequest(relay.DirectionOffRamp))
		require.Error(t, err)
		assert.Equal(t, pkgErrors.CodeRateUnavailable, codeOf(t, err))
	})
}

type fakeFonbnk struct {
	offRampAmounts []float64
	onRampAmounts  []float64
	// rate is applied to whatever fiat amount is asked for.
	rate float64
	err  error
}

func (f *fakeFonbnk) QuoteOffRamp(_ context.Context, _ fonbnk.CryptoLeg, _ fonbnk.FiatLeg, cryptoAmount float64) (*fonbnk.Quote, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.offRampAmounts = append(f.offRampAmounts, cryptoAmount)
	return &fonbnk.Quote{
		Deposit: fonbnk.QuoteLeg{CurrencyType: fonbnk.CurrencyTypeCrypto, Cashout: fonbnk.Cashout{
			AmountBeforeFees: cryptoAmount, AmountAfterFees: cryptoAmount,
		}},
		Payout: fonbnk.QuoteLeg{CurrencyType: fonbnk.CurrencyTypeFiat, Cashout: fonbnk.Cashout{
			AmountBeforeFees: cryptoAmount * f.rate, AmountAfterFees: cryptoAmount * f.rate,
		}},
	}, nil
}

func (f *fakeFonbnk) QuoteOnRamp(_ context.Context, _ fonbnk.FiatLeg, _ fonbnk.CryptoLeg, cryptoAmount float64) (*fonbnk.Quote, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.onRampAmounts = append(f.onRampAmounts, cryptoAmount)
	return &fonbnk.Quote{
		Deposit: fonbnk.QuoteLeg{CurrencyType: fonbnk.CurrencyTypeFiat, Cashout: fonbnk.Cashout{
			AmountBeforeFees: cryptoAmount * f.rate, AmountAfterFees: cryptoAmount * f.rate,
		}},
		Payout: fonbnk.QuoteLeg{CurrencyType: fonbnk.CurrencyTypeCrypto, Cashout: fonbnk.Cashout{
			AmountBeforeFees: cryptoAmount, AmountAfterFees: cryptoAmount,
		}},
	}, nil
}

func fonbnkSource(t *testing.T, client FonbnkQuoter) *FonbnkSource {
	t.Helper()
	s, err := NewFonbnkSource(FonbnkSourceConfig{
		Client:             client,
		CryptoCurrencyCode: "STELLAR_USDC",
		CarrierCodes:       map[string]string{"KES": "ke_safaricom"},
	})
	require.NoError(t, err)
	return s
}

// The amount rides the crypto leg, which is the side we always know, so one
// call prices the corridor at the size actually being moved.
func TestFonbnkSource_OffRampQuotesOnceAtTheCryptoAmount(t *testing.T) {
	client := &fakeFonbnk{rate: 123.4}

	got, err := fonbnkSource(t, client).QuoteRate(context.Background(), kesRequest(relay.DirectionOffRamp))
	require.NoError(t, err)

	require.Len(t, client.offRampAmounts, 1, "no probe round trip is needed")
	assert.Equal(t, 20.0, client.offRampAmounts[0])
	assert.InDelta(t, 123.4, got.EffectiveRate, 0.0001)
	assert.InDelta(t, 20.0, got.CryptoAmount, 0.0001)
}

func TestFonbnkSource_OnRampQuotesOnce(t *testing.T) {
	client := &fakeFonbnk{rate: 132.65}

	got, err := fonbnkSource(t, client).QuoteRate(context.Background(), kesRequest(relay.DirectionOnRamp))
	require.NoError(t, err)

	require.Len(t, client.onRampAmounts, 1)
	assert.Empty(t, client.offRampAmounts)
	assert.InDelta(t, 132.65, got.EffectiveRate, 0.0001)
}

func TestFonbnkSource_PropagatesFailure(t *testing.T) {
	client := &fakeFonbnk{err: errors.New("down")}
	_, err := fonbnkSource(t, client).QuoteRate(context.Background(), kesRequest(relay.DirectionOffRamp))
	require.Error(t, err)
}

func TestNewFonbnkSource_Validation(t *testing.T) {
	_, err := NewFonbnkSource(FonbnkSourceConfig{CryptoCurrencyCode: "STELLAR_USDC"})
	require.Error(t, err)

	_, err = NewFonbnkSource(FonbnkSourceConfig{Client: &fakeFonbnk{}})
	require.Error(t, err)
}

func TestNewYellowCardSource_Validation(t *testing.T) {
	_, err := NewYellowCardSource(YellowCardSourceConfig{})
	require.Error(t, err)
}
