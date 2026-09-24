package airtel

import "testing"

func TestNormalizeMSISDN(t *testing.T) {
	cases := map[string]struct {
		in      string
		want    MSISDN
		wantErr bool
	}{
		"e164":         {in: "+254733123456", want: "733123456"},
		"country code": {in: "254733123456", want: "733123456"},
		"trunk zero":   {in: "0733123456", want: "733123456"},
		"national":     {in: "733123456", want: "733123456"},
		"spaced":       {in: "+254 733 123 456", want: "733123456"},
		"hyphenated":   {in: "0733-123-456", want: "733123456"},
		"new range":    {in: "0100123456", want: "100123456"},
		"empty":        {in: "", wantErr: true},
		"too short":    {in: "07331234", wantErr: true},
		"too long":     {in: "07331234567", wantErr: true},
		"not mobile":   {in: "0201234567", wantErr: true},
		"letters":      {in: "not-a-number", wantErr: true},
		"foreign":      {in: "+256733123456", wantErr: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := NormalizeMSISDN(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("NormalizeMSISDN(%q) = %q, want an error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeMSISDN(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("NormalizeMSISDN(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The country code is the inversion that matters: Daraja requires it and
// Airtel forbids it, and the two rails share a platform.
func TestNormalizeMSISDN_NeverCarriesCountryCode(t *testing.T) {
	for _, in := range []string{"+254733123456", "254733123456", "0733123456"} {
		got, err := NormalizeMSISDN(in)
		if err != nil {
			t.Fatalf("NormalizeMSISDN(%q): %v", in, err)
		}
		if len(got) != nationalDigits {
			t.Fatalf("NormalizeMSISDN(%q) = %q, want %d digits", in, got, nationalDigits)
		}
		if got[0] == '2' {
			t.Fatalf("NormalizeMSISDN(%q) = %q, which still carries a country code", in, got)
		}
	}
}
