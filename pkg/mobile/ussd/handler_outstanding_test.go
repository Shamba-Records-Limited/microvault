package ussd

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type outstandingLoanSvc struct {
	fakeLoanSvc
	outstanding bool
	err         error
}

func (s outstandingLoanSvc) HasOutstandingLoan(context.Context, string) (bool, error) {
	return s.outstanding, s.err
}

func newOutstandingHarness(t *testing.T, loans outstandingLoanSvc) *USSDHandler {
	t.Helper()
	user := &fakeUserSvc{user: map[string]any{"id": "u1"}, accounts: []any{map[string]any{}}}
	h := newHarness(t, user, &fakePINSvc{hasPIN: true})
	h.loanService = loans
	return h
}

func TestRequestLoan_OutstandingLoanEndsSession(t *testing.T) {
	h := newOutstandingHarness(t, outstandingLoanSvc{outstanding: true})

	resp := dial(t, h, "o1", "", "1")
	if resp != "END "+GetLocalizedMessage("en", "loan_outstanding") {
		t.Errorf("expected loan_outstanding, got %q", resp)
	}
}

func TestRequestLoan_OutstandingCheckFailureEndsSession(t *testing.T) {
	h := newOutstandingHarness(t, outstandingLoanSvc{err: errors.New("db down")})

	resp := dial(t, h, "o2", "", "1")
	if !strings.HasPrefix(resp, "END ") || strings.Contains(resp, "Enter amount") {
		t.Errorf("a failed check must not reach the amount prompt, got %q", resp)
	}
}

func TestRequestLoan_NoOutstandingLoanShowsAmount(t *testing.T) {
	h := newOutstandingHarness(t, outstandingLoanSvc{})

	resp := dial(t, h, "o3", "", "1")
	if !strings.Contains(resp, "Enter amount to borrow") {
		t.Errorf("expected the amount prompt, got %q", resp)
	}
}

func TestLoanOutstandingString_IsGSM7(t *testing.T) {
	for _, lang := range []string{"en", "sw", "fr"} {
		msg := GetLocalizedMessage(lang, "loan_outstanding")
		for _, r := range msg {
			if r > 0x7F && !strings.ContainsRune("àäöñüèéùìòÇØøÅåÆæßÉÄÖÑÜ§¿¡£$¥¤…", r) {
				t.Errorf("%s: %q may not survive the USSD display", lang, r)
			}
		}
	}
}
