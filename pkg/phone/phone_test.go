package phone

import "testing"

func TestRedact(t *testing.T) {
	cases := map[string]string{
		"254711222111":  "254711***111",  // the canonical pattern: first 6, last 3
		"+254711222111": "+254711***111", // leading + preserved
		"254799334972":  "254799***972",
		"254711111":     "254711111", // 9 digits: nothing in the middle to mask
		"12345":         "12345",     // too short, returned unchanged
		"":              "",
	}
	for in, want := range cases {
		if got := Redact(in); got != want {
			t.Errorf("Redact(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"254711222111":  "+254711222111",
		"+254711222111": "+254711222111",
		"  254711  ":    "+254711",
		"":              "",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestE164(t *testing.T) {
	cases := map[string]string{
		"+254 711 222 111": "+254711222111", // separators stripped
		"254711222111":     "+254711222111",
		"00254711222111":   "+254711222111", // 00 international prefix
		"0711222111":       "",              // national format, not resolvable
		"":                 "",
	}
	for in, want := range cases {
		if got := E164(in); got != want {
			t.Errorf("E164(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormat(t *testing.T) {
	if got := Format("711222111", ""); got != "+254711222111" {
		t.Errorf("Format default country = %q, want +254711222111", got)
	}
	if got := Format("00254711", ""); got != "00254711" {
		t.Errorf("Format 00-prefixed left as-is = %q, want 00254711", got)
	}
}
