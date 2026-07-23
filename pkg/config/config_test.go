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

// The two issuers must name the same asset: the vault is built with
// USDC_ISSUER and refunds are repaid into it, so a MoneyGram override pointing
// elsewhere means every refund fails its on-ledger issuer check and the loan
// sits in refund_pending with no error explaining why.
func TestUSDCIssuerAlignment(t *testing.T) {
	const vault = "GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5"
	const other = "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF"

	tests := []struct {
		name      string
		moneygram string
		stellar   string
		wantErr   bool
	}{
		{name: "override unset inherits the vault issuer", stellar: vault},
		{name: "override matches", moneygram: vault, stellar: vault},
		{name: "override diverges", moneygram: other, stellar: vault, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateUSDCIssuerAlignment(tt.moneygram, tt.stellar)
			if tt.wantErr && err == nil {
				t.Fatal("a divergent issuer must fail at startup, not at refund time")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
