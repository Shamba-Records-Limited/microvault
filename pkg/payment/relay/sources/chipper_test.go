package sources

import (
	"context"
	"errors"
	"testing"

	"github.com/govalues/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/chipper"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/relay"
)

type fakeChipperRater struct {
	rate string
	err  error
}

func (f fakeChipperRater) GetRate(_ context.Context, origin, destination string) (*chipper.Rate, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &chipper.Rate{From: origin, To: destination, Rate: decimal.MustParse(f.rate)}, nil
}

func sandboxFees(t *testing.T) *chipper.FeeSchedule {
	t.Helper()
	s, err := chipper.ParseFeeSchedule(`{"deposit":{"pct":"1.5"},"payouts":{"ke_mpesa":{"pct":"1.5"}}}`)
	require.NoError(t, err)
	return s
}

func TestChipperSource(t *testing.T) {
	src, err := NewChipperSource(ChipperSourceConfig{
		Client:      fakeChipperRater{rate: "130.226805"},
		Fees:        sandboxFees(t),
		MethodCodes: map[string]string{"kes": "ke_mpesa"},
	})
	require.NoError(t, err)
	assert.True(t, src.SupportsDirection(relay.DirectionOffRamp))
	assert.False(t, src.SupportsDirection(relay.DirectionOnRamp))

	q, err := src.QuoteRate(context.Background(), relay.RateRequest{
		Direction: relay.DirectionOffRamp, FiatCurrency: "KES", CountryCode: "KE", CryptoAmount: 100,
	})
	require.NoError(t, err)
	assert.InDelta(t, 97*130.226805, q.FiatAmount, 1e-6, "both 1.5% legs come off before conversion")
	assert.InDelta(t, 0.97*130.226805, q.EffectiveRate, 1e-9)
	assert.Equal(t, chipper.ProviderName, q.Provider)

	_, err = src.QuoteRate(context.Background(), relay.RateRequest{Direction: relay.DirectionOffRamp, FiatCurrency: "GHS", CryptoAmount: 100})
	require.Error(t, err)
	assert.Equal(t, pkgErrors.CodeCorridorUnavailable, codeOf(t, err))

	failing, err := NewChipperSource(ChipperSourceConfig{Client: fakeChipperRater{err: errors.New("503")}, Fees: sandboxFees(t), MethodCodes: map[string]string{"KES": "ke_mpesa"}})
	require.NoError(t, err)
	_, err = failing.QuoteRate(context.Background(), relay.RateRequest{Direction: relay.DirectionOffRamp, FiatCurrency: "KES", CryptoAmount: 100})
	require.Error(t, err)
	assert.Equal(t, pkgErrors.CodeRateUnavailable, codeOf(t, err))
}

func TestNewChipperSourceRequiresFeeRules(t *testing.T) {
	_, err := NewChipperSource(ChipperSourceConfig{Client: fakeChipperRater{rate: "1"}, Fees: sandboxFees(t), MethodCodes: map[string]string{"UGX": "ug_mtn"}})
	require.Error(t, err)
	assert.Equal(t, pkgErrors.CodeInvalidConfig, codeOf(t, err))

	_, err = NewChipperSource(ChipperSourceConfig{Client: fakeChipperRater{rate: "1"}, MethodCodes: map[string]string{"KES": "ke_mpesa"}})
	require.Error(t, err)
}
