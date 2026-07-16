package config

import "testing"

// TestMoneyGramSecretResolution documents the invariant that an explicitly
// configured MONEYGRAM_AUTH_SECRET / MONEYGRAM_FUNDS_SECRET overrides the
// TREASURY_SECRET_KEY default — it does not fall back to treasury when set.
// Mirrors the exact call sites in Load().
func TestMoneyGramSecretResolution(t *testing.T) {
	const treasury = "STREASURY"
	const dedicatedAuth = "SAUTHDEDICATED"
	const dedicatedFunds = "SFUNDSDEDICATED"

	tests := []struct {
		name      string
		envAuth   string
		envFunds  string
		wantAuth  string
		wantFunds string
	}{
		{
			name:      "both unset default to treasury",
			wantAuth:  treasury,
			wantFunds: treasury,
		},
		{
			name:      "explicit auth wins, funds still defaults",
			envAuth:   dedicatedAuth,
			wantAuth:  dedicatedAuth,
			wantFunds: treasury,
		},
		{
			name:      "explicit funds wins, auth still defaults",
			envFunds:  dedicatedFunds,
			wantAuth:  treasury,
			wantFunds: dedicatedFunds,
		},
		{
			name:      "both explicit override treasury",
			envAuth:   dedicatedAuth,
			envFunds:  dedicatedFunds,
			wantAuth:  dedicatedAuth,
			wantFunds: dedicatedFunds,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotAuth := firstNonEmpty(tc.envAuth, treasury)
			gotFunds := firstNonEmpty(tc.envFunds, treasury)
			if gotAuth != tc.wantAuth {
				t.Errorf("auth: got %q, want %q", gotAuth, tc.wantAuth)
			}
			if gotFunds != tc.wantFunds {
				t.Errorf("funds: got %q, want %q", gotFunds, tc.wantFunds)
			}
		})
	}
}
