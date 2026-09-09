package config

import (
	"strings"
	"testing"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/mpesa"
)

// validMpesa is a production-complete config; each case breaks one thing.
func validMpesa() MpesaConfig {
	return MpesaConfig{
		CollectionShortcode:    174379,
		Passkey:                "passkey",
		InitiatorName:          "testapi",
		InitiatorPassword:      "initiator-password",
		CallbackBaseURL:        "https://microvault.example.app",
		CallbackSlug:           "a1b2c3",
		CallbackAllowedCIDRs:   []string{"196.201.214.0/24"},
		SettlementMode:         MpesaSettlementOTC,
		NumberValidationPolicy: mpesa.ValidationDisabled,
	}
}

func TestMpesaConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		mutate  func(*MpesaConfig)
		wantErr string
	}{
		{
			name: "complete production config passes",
			env:  "production",
		},
		{
			name:   "sandbox tolerates the production-only values being absent",
			env:    "development",
			mutate: func(c *MpesaConfig) { *c = MpesaConfig{SettlementMode: MpesaSettlementOTC} },
		},
		{
			name:    "provider_sweep is rejected in production",
			env:     "production",
			mutate:  func(c *MpesaConfig) { c.SettlementMode = MpesaSettlementProviderSweep },
			wantErr: "not implemented",
		},
		{
			// The mode is unbuilt everywhere, so it must not pass merely
			// because the deploy is a sandbox one.
			name:    "provider_sweep is rejected outside production too",
			env:     "development",
			mutate:  func(c *MpesaConfig) { c.SettlementMode = MpesaSettlementProviderSweep },
			wantErr: "not implemented",
		},
		{
			name:    "unknown settlement mode is rejected",
			env:     "development",
			mutate:  func(c *MpesaConfig) { c.SettlementMode = "anchor" },
			wantErr: "MPESA_SETTLEMENT_MODE",
		},
		{
			name:    "unknown validation policy is rejected",
			env:     "development",
			mutate:  func(c *MpesaConfig) { c.NumberValidationPolicy = "maybe" },
			wantErr: "MPESA_NUMBER_VALIDATION_POLICY",
		},
		{
			name:    "callback base carrying a Daraja-blocked word is rejected",
			env:     "development",
			mutate:  func(c *MpesaConfig) { c.CallbackBaseURL = "https://mpesa.example.app" },
			wantErr: "MPESA_CALLBACK_BASE_URL",
		},
		{
			name:    "plaintext callback base is rejected",
			env:     "development",
			mutate:  func(c *MpesaConfig) { c.CallbackBaseURL = "http://microvault.example.app" },
			wantErr: "must be https",
		},
		{
			name:    "production requires the callback slug",
			env:     "production",
			mutate:  func(c *MpesaConfig) { c.CallbackSlug = "" },
			wantErr: "MPESA_CALLBACK_SLUG",
		},
		{
			name:    "production requires an egress allowlist",
			env:     "production",
			mutate:  func(c *MpesaConfig) { c.CallbackAllowedCIDRs = nil },
			wantErr: "MPESA_CALLBACK_ALLOWED_CIDRS",
		},
		{
			name:    "production names every missing value at once",
			env:     "production",
			mutate:  func(c *MpesaConfig) { c.Passkey, c.InitiatorName = "", "" },
			wantErr: "MPESA_PASSKEY, MPESA_INITIATOR_NAME",
		},
		{
			// ParseUint swallows an absent or unparseable shortcode into 0,
			// so the range check is the only thing that catches it.
			name:    "production rejects an unset shortcode",
			env:     "production",
			mutate:  func(c *MpesaConfig) { c.CollectionShortcode = 0 },
			wantErr: "5-7 digit shortcode",
		},
		{
			name:    "production rejects an over-long shortcode",
			env:     "production",
			mutate:  func(c *MpesaConfig) { c.CollectionShortcode = 12345678 },
			wantErr: "5-7 digit shortcode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validMpesa()
			if tt.mutate != nil {
				tt.mutate(&cfg)
			}
			err := cfg.Validate(tt.env)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate(%q) = %v, want nil", tt.env, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate(%q) = nil, want error containing %q", tt.env, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate(%q) = %q, want it to contain %q", tt.env, err, tt.wantErr)
			}
		})
	}
}
