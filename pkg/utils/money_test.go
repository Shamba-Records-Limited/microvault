package utils

import "testing"

func TestBasisPoints(t *testing.T) {
	if got := ToBps(15.0); got != 1500 {
		t.Errorf("ToBps(15.0) = %d, want 1500", got)
	}
	if got := ToBps(3.75); got != 375 {
		t.Errorf("ToBps(3.75) = %d, want 375", got)
	}
	if got := BasisPoints(1500).ToPercent(); got != 15.0 {
		t.Errorf("ToPercent() = %v, want 15.0", got)
	}
	if got := BasisPoints(1500).ToDecimal(); got != 0.15 {
		t.Errorf("ToDecimal() = %v, want 0.15", got)
	}
	if got := BasisPoints(1500).String(); got != "15.00%" {
		t.Errorf("String() = %q, want 15.00%%", got)
	}
}

func TestInt64AmountRoundTrip(t *testing.T) {
	amt, err := Int64ToAmount(100050, "KES")
	if err != nil {
		t.Fatalf("Int64ToAmount error: %v", err)
	}
	back, err := AmountToInt64(amt)
	if err != nil {
		t.Fatalf("AmountToInt64 error: %v", err)
	}
	if back != 100050 {
		t.Errorf("round trip = %d, want 100050", back)
	}
}

func TestInt64ToAmount_InvalidCurrency(t *testing.T) {
	if _, err := Int64ToAmount(100, "USDC"); err == nil {
		t.Error("expected error for non-ISO currency USDC, got nil")
	}
}

func TestFormatAmount(t *testing.T) {
	// Valid ISO currency renders the decimal value.
	if got := FormatAmount(100050, "KES"); got == "" || got == "100050 KES (smallest unit)" {
		t.Errorf("FormatAmount(valid) = %q, want a formatted amount", got)
	}
	// Invalid currency falls back to the raw-units form.
	if got := FormatAmount(100050, "USDC"); got != "100050 USDC (smallest unit)" {
		t.Errorf("FormatAmount(invalid) = %q, want fallback", got)
	}
}

func TestParseAmount(t *testing.T) {
	got, err := ParseAmount("1000.50", "KES")
	if err != nil {
		t.Fatalf("ParseAmount error: %v", err)
	}
	if got != 100050 {
		t.Errorf("ParseAmount = %d, want 100050", got)
	}
	if _, err := ParseAmount("not-a-number", "KES"); err == nil {
		t.Error("expected error parsing garbage, got nil")
	}
}

func TestAddSubtractAmounts(t *testing.T) {
	sum, err := AddAmounts(100050, 50, "KES")
	if err != nil || sum != 100100 {
		t.Errorf("AddAmounts = %d (err %v), want 100100", sum, err)
	}
	diff, err := SubtractAmounts(100050, 50, "KES")
	if err != nil || diff != 100000 {
		t.Errorf("SubtractAmounts = %d (err %v), want 100000", diff, err)
	}
	if _, err := AddAmounts(1, 1, "USDC"); err == nil {
		t.Error("AddAmounts expected error for invalid currency")
	}
}

func TestMultiplyAmount(t *testing.T) {
	got, err := MultiplyAmount(100000, 2.5, "KES")
	if err != nil || got != 250000 {
		t.Errorf("MultiplyAmount = %d (err %v), want 250000", got, err)
	}
}

func TestApplyBasisPoints(t *testing.T) {
	got, err := ApplyBasisPoints(100000, 350, "KES")
	if err != nil || got != 3500 {
		t.Errorf("ApplyBasisPoints = %d (err %v), want 3500", got, err)
	}
}

func TestCalculateSimpleInterest(t *testing.T) {
	// 1000.00 at 12% for a full year = 120.00.
	got, err := CalculateSimpleInterest(100000, 1200, 365, "KES")
	if err != nil || got != 12000 {
		t.Errorf("CalculateSimpleInterest = %d (err %v), want 12000", got, err)
	}
}

func TestCalculateCompoundInterest(t *testing.T) {
	// dailyRateBps = 36500/365 = 100 bps = 1%/day. One day on 1000.00 = 1010.00.
	got, err := CalculateCompoundInterest(100000, 36500, 1, "KES")
	if err != nil || got != 101000 {
		t.Errorf("CalculateCompoundInterest(1 day) = %d (err %v), want 101000", got, err)
	}
	// Zero days returns the principal untouched.
	got, err = CalculateCompoundInterest(100000, 36500, 0, "KES")
	if err != nil || got != 100000 {
		t.Errorf("CalculateCompoundInterest(0 days) = %d (err %v), want 100000", got, err)
	}
}

func TestConvertCurrency(t *testing.T) {
	// 1000.00 KES at 0.77 = 770.00 USD.
	got, err := ConvertCurrency(100000, "KES", "USD", 7700)
	if err != nil || got != 77000 {
		t.Errorf("ConvertCurrency = %d (err %v), want 77000", got, err)
	}
}

func TestCompareAmounts(t *testing.T) {
	cases := []struct {
		a, b int64
		want int
	}{
		{100, 200, -1},
		{200, 100, 1},
		{100, 100, 0},
	}
	for _, c := range cases {
		got, err := CompareAmounts(c.a, c.b, "KES")
		if err != nil || got != c.want {
			t.Errorf("CompareAmounts(%d,%d) = %d (err %v), want %d", c.a, c.b, got, err, c.want)
		}
	}
}

func TestSignHelpers(t *testing.T) {
	if !IsPositive(1) || IsPositive(0) || IsPositive(-1) {
		t.Error("IsPositive wrong")
	}
	if !IsNegative(-1) || IsNegative(0) || IsNegative(1) {
		t.Error("IsNegative wrong")
	}
	if !IsZero(0) || IsZero(1) {
		t.Error("IsZero wrong")
	}
}

func TestInvalidCurrencyErrors(t *testing.T) {
	// Every arithmetic helper must surface an error for a non-ISO currency
	// rather than silently returning a zero amount.
	checks := []struct {
		name string
		call func() error
	}{
		{"Subtract", func() error { _, e := SubtractAmounts(1, 1, "USDC"); return e }},
		{"Multiply", func() error { _, e := MultiplyAmount(1, 2, "USDC"); return e }},
		{"ApplyBps", func() error { _, e := ApplyBasisPoints(1, 10, "USDC"); return e }},
		{"SimpleInterest", func() error { _, e := CalculateSimpleInterest(1, 10, 30, "USDC"); return e }},
		{"CompoundInterest", func() error { _, e := CalculateCompoundInterest(1, 10, 30, "USDC"); return e }},
		{"Convert", func() error { _, e := ConvertCurrency(1, "USDC", "USD", 7700); return e }},
		{"Compare", func() error { _, e := CompareAmounts(1, 2, "USDC"); return e }},
	}
	for _, c := range checks {
		if err := c.call(); err == nil {
			t.Errorf("%s: expected error for invalid currency, got nil", c.name)
		}
	}
}

func TestValidateCurrency(t *testing.T) {
	for _, c := range []string{"KES", "USD", "EUR"} {
		if !ValidateCurrency(c) {
			t.Errorf("ValidateCurrency(%q) = false, want true", c)
		}
	}
	for _, c := range []string{"USDC", "ZZZ", ""} {
		if ValidateCurrency(c) {
			t.Errorf("ValidateCurrency(%q) = true, want false", c)
		}
	}
}
