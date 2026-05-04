package moneygram

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChildAccountMemo_Deterministic(t *testing.T) {
	pubkey := "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"

	first := ChildAccountMemo(pubkey, 42)
	second := ChildAccountMemo(pubkey, 42)

	assert.Equal(t, first, second, "same input must yield same memo across calls")
}

func TestChildAccountMemo_AlwaysPositive(t *testing.T) {
	// Hammer with diverse inputs — any negative result is a sign-bit-mask bug.
	pubkeys := []string{
		"GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
		"GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5",
		"GD5NUMEX7LYHXGXCAD4PGW7JDMOUY2DKRGY5XZHJS5IONVHDKCJYGVCL",
		"",
	}
	for _, pk := range pubkeys {
		for i := uint32(0); i < 100_000; i++ {
			memo := ChildAccountMemo(pk, i)
			assert.GreaterOrEqual(t, memo, int64(0), "memo must be positive (pk=%q, i=%d)", pk, i)
		}
	}
}

func TestChildAccountMemo_TreasuryScoped(t *testing.T) {
	// Different treasury pubkeys with the same account index must produce
	// different memos — otherwise testnet and mainnet share user memo space.
	mainnet := "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"
	testnet := "GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5"

	assert.NotEqual(t, ChildAccountMemo(mainnet, 1), ChildAccountMemo(testnet, 1))
	assert.NotEqual(t, ChildAccountMemo(mainnet, 999), ChildAccountMemo(testnet, 999))
}

func TestChildAccountMemo_IndexDistinct(t *testing.T) {
	pubkey := "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"

	// Adjacent indices must not collapse to the same memo.
	seen := make(map[int64]uint32, 10_000)
	for i := uint32(0); i < 10_000; i++ {
		memo := ChildAccountMemo(pubkey, i)
		if prev, ok := seen[memo]; ok {
			t.Fatalf("collision: index %d and %d both produced memo %d", prev, i, memo)
		}
		seen[memo] = i
	}
}
