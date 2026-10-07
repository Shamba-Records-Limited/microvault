package pilot

import "testing"

func TestNormalizePhone(t *testing.T) {
	cases := map[string]string{
		"0722 000 111":  "254722000111",
		"254722000111":  "254722000111",
		"+254722000111": "254722000111",
		"+256772000111": "256772000111",
		"0722":          "",
	}
	for in, want := range cases {
		if got := NormalizePhone(in); got != want {
			t.Errorf("NormalizePhone(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeNationalID(t *testing.T) {
	cases := []struct{ input, expected string }{
		{"\t1234 5678\n", "12345678"},
		{"a1234567", "A1234567"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := NormalizeNationalID(tc.input); got != tc.expected {
			t.Errorf("NormalizeNationalID(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}
