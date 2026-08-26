package ussd

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/moneygram"
)

// The floor used to be written out in stroops here and in dollars in the
// moneygram package — two places for one number. These pin the derivation so a
// limit change in one place cannot silently disagree with the other.
func TestDepositStroopsTrackTheUSDConstants(t *testing.T) {
	assert.Equal(t, int64(150_000_000), MinMoneyGramDepositStroops, "15 USDC at seven decimals")
	assert.Equal(t, int64(9_500_000_000), MaxMoneyGramDepositStroops, "950 USDC at seven decimals")
	assert.Equal(t, moneygram.MinDepositUSD*1e7, float64(MinMoneyGramDepositStroops))
	assert.Equal(t, moneygram.MaxDepositUSD*1e7, float64(MaxMoneyGramDepositStroops))
}

// The cash-in ceiling was previously unbounded: a payoff of any size offered
// the MoneyGram rail, and the anchor refused it at the counter after the
// borrower had travelled there.
func TestCashRailEligible(t *testing.T) {
	cases := []struct {
		name    string
		stroops int64
		want    bool
	}{
		{"below the floor", MinMoneyGramDepositStroops - 1, false},
		{"exactly the floor", MinMoneyGramDepositStroops, true},
		{"mid-range", 5_000_000_000, true},
		{"exactly the ceiling", MaxMoneyGramDepositStroops, true},
		{"above the ceiling", MaxMoneyGramDepositStroops + 1, false},
		{"far above the ceiling", 50_000_000_000, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			choice := repayLoanChoice{PayoffStroops: c.stroops}
			assert.Equal(t, c.want, choice.cashRailEligible())
		})
	}
}

// A KES 3,000 loan is roughly 23 USDC, so its payoff clears the floor. This is
// the shape the seeded product produces today.
func TestCashRailEligible_SeededProductPayoffIsInRange(t *testing.T) {
	choice := repayLoanChoice{PayoffStroops: 240_000_000} // ~24 USDC
	assert.True(t, choice.cashRailEligible())
}
