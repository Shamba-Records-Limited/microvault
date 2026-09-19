package config

import (
	"strings"
	"testing"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/airtel"
)

// validAirtel is a production-complete config; each case breaks one thing.
func validAirtel() AirtelConfig {
	return AirtelConfig{
		ClientID:             "client-id",
		ClientSecret:         "client-secret",
		Environment:          airtel.EnvironmentProduction,
		Country:              "KE",
		Currency:             "KES",
		CallbackBaseURL:      "https://microvault.example.app",
		CallbackSlug:         "unguessable",
		CallbackAllowedCIDRs: []string{"41.0.0.0/8"},
		EnquiryDelay:         EnquiryDelayFloor,
		SettlementMode:       AirtelSettlementOTC,
	}
}

func TestAirtelConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		mutate  func(*AirtelConfig)
		wantErr string
	}{
		{name: "production complete", env: "production"},
		{name: "development complete", env: "development"},
		{
			name:   "development tolerates an unconfigured rail",
			env:    "development",
			mutate: func(c *AirtelConfig) { *c = AirtelConfig{} },
		},
		{
			// An absent rail is not a misconfigured one. A deployment with no
			// Airtel account must still boot in production.
			name:   "production tolerates an unconfigured rail",
			env:    "production",
			mutate: func(c *AirtelConfig) { *c = AirtelConfig{} },
		},
		{
			name:    "rejects an unknown environment",
			env:     "development",
			mutate:  func(c *AirtelConfig) { c.Environment = "uat" },
			wantErr: "AIRTEL_ENVIRONMENT",
		},
		{
			name:    "rejects an unknown settlement mode",
			env:     "development",
			mutate:  func(c *AirtelConfig) { c.SettlementMode = "anchor" },
			wantErr: "AIRTEL_SETTLEMENT_MODE",
		},
		{
			// Not a production-only check: selecting an unbuilt mode is
			// wrong everywhere, and failing only in production would let it
			// pass review on a staging deploy.
			name:    "rejects the unbuilt settlement mode in development too",
			env:     "development",
			mutate:  func(c *AirtelConfig) { c.SettlementMode = AirtelSettlementDisbursement },
			wantErr: "not implemented",
		},
		{
			name:    "rejects a non-https callback base",
			env:     "development",
			mutate:  func(c *AirtelConfig) { c.CallbackBaseURL = "http://microvault.example.app" },
			wantErr: "must be https",
		},
		{
			// Airtel documents a three-minute floor. Below it the answer is
			// uninformative and every round costs a call.
			name:    "production rejects an enquiry delay below the floor",
			env:     "production",
			mutate:  func(c *AirtelConfig) { c.EnquiryDelay = time.Second },
			wantErr: "AIRTEL_ENQUIRY_DELAY",
		},
		{
			// Outside production a short delay is how the rail is tested by
			// hand; a three-minute wait per attempt makes it unusable.
			name:   "development allows an enquiry delay below the floor",
			env:    "development",
			mutate: func(c *AirtelConfig) { c.EnquiryDelay = time.Second },
		},
		{
			name:    "production requires a callback slug",
			env:     "production",
			mutate:  func(c *AirtelConfig) { c.CallbackSlug = "" },
			wantErr: "AIRTEL_CALLBACK_SLUG",
		},
		{
			name:    "production requires a callback base",
			env:     "production",
			mutate:  func(c *AirtelConfig) { c.CallbackBaseURL = "" },
			wantErr: "AIRTEL_CALLBACK_BASE_URL",
		},
		{
			// Airtel does not publish an egress list, so this is the check
			// that keeps production blocked until their support supplies one.
			name:    "production requires an egress allowlist",
			env:     "production",
			mutate:  func(c *AirtelConfig) { c.CallbackAllowedCIDRs = nil },
			wantErr: "AIRTEL_CALLBACK_ALLOWED_CIDRS",
		},
		{
			name:    "production rejects the staging environment",
			env:     "production",
			mutate:  func(c *AirtelConfig) { c.Environment = airtel.EnvironmentStaging },
			wantErr: "must be \"production\" on a production deployment",
		},
		{
			name:   "development tolerates a missing slug",
			env:    "development",
			mutate: func(c *AirtelConfig) { c.CallbackSlug = "" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validAirtel()
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

func TestAirtelConfig_Enabled(t *testing.T) {
	cases := map[string]struct {
		cfg  AirtelConfig
		want bool
	}{
		"both set":  {cfg: AirtelConfig{ClientID: "a", ClientSecret: "b"}, want: true},
		"no secret": {cfg: AirtelConfig{ClientID: "a"}},
		"no id":     {cfg: AirtelConfig{ClientSecret: "b"}},
		"neither":   {cfg: AirtelConfig{}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.cfg.Enabled(); got != tc.want {
				t.Fatalf("Enabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAirtelConfig_CallbackURL(t *testing.T) {
	cases := map[string]struct {
		base string
		slug string
		want string
	}{
		"plain":          {base: "https://x.test", slug: "abc", want: "https://x.test/api/v1/callbacks/airtel/abc/collection"},
		"trailing slash": {base: "https://x.test/", slug: "abc", want: "https://x.test/api/v1/callbacks/airtel/abc/collection"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := AirtelConfig{CallbackBaseURL: tc.base, CallbackSlug: tc.slug}
			if got := cfg.AirtelCallbackURL(); got != tc.want {
				t.Fatalf("AirtelCallbackURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The floor is Airtel's own documented figure and the client's EnquiryFloor
// constant. Two encodings of one fact must not drift.
func TestEnquiryDelayFloorMatchesTheClient(t *testing.T) {
	if EnquiryDelayFloor != airtel.EnquiryFloor {
		t.Fatalf("config floor %s does not match the client's %s", EnquiryDelayFloor, airtel.EnquiryFloor)
	}
}
