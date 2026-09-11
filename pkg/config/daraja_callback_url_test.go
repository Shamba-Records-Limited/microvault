package config

import "testing"

func TestMpesaConfig_DarajaCallbackURL(t *testing.T) {
	cases := []struct {
		name   string
		base   string
		slug   string
		suffix string
		want   string
	}{
		{
			name:   "trims a trailing slash on the base",
			base:   "https://example.com/",
			slug:   "slug123",
			suffix: "balance/result",
			want:   "https://example.com/api/v1/callbacks/daraja/slug123/balance/result",
		},
		{
			name:   "no trailing slash on the base",
			base:   "https://example.com",
			slug:   "slug123",
			suffix: "balance/timeout",
			want:   "https://example.com/api/v1/callbacks/daraja/slug123/balance/timeout",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := MpesaConfig{CallbackBaseURL: tc.base, CallbackSlug: tc.slug}
			got := cfg.DarajaCallbackURL(tc.suffix)
			if got != tc.want {
				t.Errorf("DarajaCallbackURL(%q) = %q, want %q", tc.suffix, got, tc.want)
			}
		})
	}
}
