package phone

import "testing"

func TestKenyaOperatorByPrefix(t *testing.T) {
	cases := map[string]KenyaOperator{
		"+254722000111":  OperatorSafaricom,
		"254711000111":   OperatorSafaricom,
		"0110 000 111":   OperatorSafaricom,
		"0799000111":     OperatorSafaricom,
		"0758000111":     OperatorSafaricom,
		"+254733000111":  OperatorAirtel,
		"0100000111":     OperatorAirtel,
		"0752000111":     OperatorAirtel,
		"0785000111":     OperatorAirtel,
		"0762000111":     OperatorAirtel,
		"0772000111":     OperatorTelkom,
		"0764000111":     OperatorEquitel,
		"0747000111":     OperatorUnknown,
		"0744000111":     OperatorUnknown,
		"0200000111":     OperatorUnknown,
		"+256772000111":  OperatorUnknown,
		"0722":           OperatorUnknown,
		"":               OperatorUnknown,
		"+2547220001111": OperatorUnknown,
	}
	for number, want := range cases {
		if got := KenyaOperatorByPrefix(number); got != want {
			t.Errorf("KenyaOperatorByPrefix(%q) = %q, want %q", number, got, want)
		}
	}
}

func TestKenyaE164(t *testing.T) {
	cases := map[string]string{
		"+254722000111": "+254722000111",
		"254722000111":  "+254722000111",
		"0722 000 111":  "+254722000111",
		"0110-000-111":  "+254110000111",
		"722000111":     "+254722000111",
		"+256772000111": "",
		"0722":          "",
		"":              "",
	}
	for number, want := range cases {
		if got := KenyaE164(number); got != want {
			t.Errorf("KenyaE164(%q) = %q, want %q", number, got, want)
		}
	}
}
