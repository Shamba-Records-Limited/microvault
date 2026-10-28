package ussd

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

var policyTerms = LoanTerms{MaxAPRBps: 3_600, LateFeeBps: 500, DefaultPenaltyBps: 1_500, PenaltyTierDays: []int32{7, 14, 21, 30}}

// termsLoanService adds a borrower policy to the harness loan service.
type termsLoanService struct {
	LoanService
	terms *LoanTerms
}

func (s termsLoanService) GetLoanTerms(context.Context) (*LoanTerms, error) { return s.terms, nil }

func TestLoanTermsText_FitsTheScreenWithNavigationInEveryLanguage(t *testing.T) {
	for _, lang := range []string{"en", "sw", "fr"} {
		body := LoanTermsText(lang, "KES", 300_000, 30, time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC), policyTerms)
		hint := GetLocalizedMessage(lang, "nav_hint_both")
		if n := utf8.RuneCountInString(body) + 1 + utf8.RuneCountInString(hint); n > navHintBudget {
			t.Errorf("%s: %d characters with navigation, budget %d:\n%s", lang, n, navHintBudget, body)
		}
		if strings.ContainsAny(body, "é€ç") {
			t.Errorf("%s: outside GSM 03.38:\n%s", lang, body)
		}
	}
}

func TestLoanTermsText_MaxRepayAndTiers(t *testing.T) {
	body := LoanTermsText("en", "KES", 5_000, 30, time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC), policyTerms)
	// 5000 × e^(0.36 × 30/365) = 5150.0…, rounded up.
	for _, want := range []string{"36%/yr", "KES 5151", "27/11", "+5%/yr at day 7, 14, 21", "+15%/yr more at day 30", "1. Accept"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func newTermsHarness(t *testing.T, terms *LoanTerms) *USSDHandler {
	t.Helper()
	h := newBorrowHarness(t)
	h.loanService = termsLoanService{LoanService: h.loanService, terms: terms}
	return h
}

func TestLoanTerms_ShownBeforeThePIN(t *testing.T) {
	h := newTermsHarness(t, &policyTerms)

	resp := dial(t, h, "t1", "", "1", "2000")
	if !strings.Contains(resp, "Interest up to 36%/yr") || strings.Contains(resp, "Enter PIN") {
		t.Fatalf("expected the terms screen before the PIN, got %q", resp)
	}
	resp = dial(t, h, "t1", "1")
	if !strings.Contains(resp, "Enter PIN") {
		t.Fatalf("accepting the terms should reach the PIN screen, got %q", resp)
	}
	resp = dial(t, h, "t1", "0")
	if !strings.Contains(resp, "Interest up to") {
		t.Fatalf("back from the PIN screen should return to the terms, got %q", resp)
	}
	resp = dial(t, h, "t1", "0")
	if !strings.Contains(resp, "Enter amount to borrow") {
		t.Fatalf("back from the terms (mobile money off) should return to the amount, got %q", resp)
	}
}

func TestLoanTerms_SkippedWithoutAPolicy(t *testing.T) {
	h := newTermsHarness(t, nil)

	resp := dial(t, h, "t2", "", "1", "2000")
	if !strings.Contains(resp, "Enter PIN") {
		t.Fatalf("no policy: straight to the PIN, got %q", resp)
	}
	resp = dial(t, h, "t2", "0")
	if !strings.Contains(resp, "Enter amount to borrow") {
		t.Fatalf("back with no terms step must not loop on the PIN screen, got %q", resp)
	}
}
