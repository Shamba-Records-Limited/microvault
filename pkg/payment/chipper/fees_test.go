package chipper

import (
	"testing"

	"github.com/govalues/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFeeSchedule(t *testing.T) {
	s, err := ParseFeeSchedule(`{"deposit":{"pct":"1.5"},"payouts":{"KE_MPESA":{"pct":"1.5","min":"0.10","max":"5"}}}`)
	require.NoError(t, err)

	rule, ok := s.PayoutRule("ke_mpesa")
	require.True(t, ok, "method codes are case-insensitive")

	tests := []struct {
		amount, want string
	}{
		{"7.678911", "0.115183665"},
		{"1", "0.10"},
		{"1000", "5"},
	}
	for _, tt := range tests {
		fee, err := rule.Fee(decimal.MustParse(tt.amount))
		require.NoError(t, err)
		assert.Equal(t, decimal.MustParse(tt.want).Trim(0).String(), fee.Trim(0).String(), tt.amount)
	}

	deposit, err := s.Deposit.Fee(decimal.MustParse("500"))
	require.NoError(t, err)
	assert.Equal(t, "7.5", deposit.Trim(0).String())

	for _, bad := range []string{
		`nope`,
		`{"deposit":{"pct":"1.5"},"payouts":{}}`,
		`{"deposit":{"pct":"-1"},"payouts":{"ke_mpesa":{"pct":"1"}}}`,
		`{"payouts":{"ke_mpesa":{"pct":"100"}}}`,
		`{"payouts":{"ke_mpesa":{"pct":"1","min":"5","max":"1"}}}`,
	} {
		_, err := ParseFeeSchedule(bad)
		assert.Error(t, err, bad)
	}
}
