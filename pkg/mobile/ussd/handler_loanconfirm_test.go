package ussd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// pinStub overrides only VerifyPIN and GetRemainingAttempts so a test can drive
// the wrong-PIN and lockout branches.
type pinStub struct {
	*fakePINSvc
	ok        bool
	remaining int
}

func (p *pinStub) VerifyPIN(context.Context, string, string) (bool, error) { return p.ok, nil }
func (p *pinStub) GetRemainingAttempts(context.Context, string) (int, error) {
	return p.remaining, nil
}

func newConfirmHarness(t *testing.T, pinSvc PINService) *USSDHandler {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	sm := NewSessionManager(client, 5*time.Minute)

	reg := NewMenuRegistry()
	NewStandardLoanMenuPreset().Initialize(reg)

	user := &fakeUserSvc{user: map[string]any{"id": "u1"}, accounts: []any{map[string]any{}}}
	return NewUSSDHandler(HandlerDeps{
		SessionManager: sm,
		MenuRegistry:   reg,
		UserService:    user,
		LoanService:    fakeLoanSvc{},
		RateService:    fakeRateSvc{},
		PINService:     pinSvc,
	})
}

func confirmSession() *Session {
	return &Session{
		SessionID:   "sess-confirm",
		PhoneNumber: "254711000111",
		UserID:      "u1",
		CurrentMenu: "loan_confirm",
		Language:    "en",
		Data: map[string]any{
			"loan_amount_local":  100000,
			"local_currency":     "KES",
			"loan_duration":      30,
			"product_id":         "p1",
			"payout_method":      "mobile_money",
			"repayment_schedule": "lump_sum",
		},
	}
}

// The terms and the PIN prompt must arrive together: a user should never be
// asked to authorize a loan on a screen that no longer shows its terms.
func TestLoanConfirm_ShowsTermsAndPINPromptTogether(t *testing.T) {
	h := newConfirmHarness(t, &pinStub{fakePINSvc: &fakePINSvc{hasPIN: true}, ok: true, remaining: 3})
	session := confirmSession()

	resp, err := h.showLoanConfirmation(context.Background(), session)
	if err != nil {
		t.Fatalf("showLoanConfirmation: %v", err)
	}
	for _, want := range []string{"1000", "30 days", "Enter PIN"} {
		if !strings.Contains(resp, want) {
			t.Errorf("confirmation screen missing %q:\n%s", want, resp)
		}
	}
	// The old two-step wording is gone; "0" is the global back input, and the
	// screen previously mislabelled it as Cancel.
	if strings.Contains(resp, "1. Confirm") || strings.Contains(resp, "Cancel") {
		t.Errorf("confirmation screen still offers a separate confirm keystroke:\n%s", resp)
	}
}

// A mistyped PIN must not cancel the loan. Losing the flow on a typo costs the
// user a metered session and their place in it.
func TestLoanConfirm_MalformedInputRepromptsRatherThanCancelling(t *testing.T) {
	h := newConfirmHarness(t, &pinStub{fakePINSvc: &fakePINSvc{hasPIN: true}, ok: true, remaining: 3})

	for _, input := range []string{"1", "12", "12345", "abcd", ""} {
		session := confirmSession()
		resp, err := h.handleLoanConfirm(context.Background(), session, input)
		if err != nil {
			t.Fatalf("input %q: %v", input, err)
		}
		if !strings.HasPrefix(resp, "CON ") {
			t.Errorf("input %q: session should stay open, got %q", input, resp)
		}
		if !strings.Contains(resp, "Enter PIN") {
			t.Errorf("input %q: expected a re-prompt, got %q", input, resp)
		}
		if !strings.Contains(resp, "30 days") {
			t.Errorf("input %q: re-prompt dropped the loan terms: %q", input, resp)
		}
	}
}

// A wrong PIN keeps the terms on screen alongside the warning.
func TestLoanConfirm_WrongPINKeepsTermsVisible(t *testing.T) {
	h := newConfirmHarness(t, &pinStub{fakePINSvc: &fakePINSvc{hasPIN: true}, ok: false, remaining: 2})
	session := confirmSession()

	resp, err := h.handleLoanConfirm(context.Background(), session, "4321")
	if err != nil {
		t.Fatalf("handleLoanConfirm: %v", err)
	}
	if !strings.Contains(resp, "Wrong PIN") {
		t.Errorf("expected the wrong-PIN warning, got %q", resp)
	}
	if !strings.Contains(resp, "2") {
		t.Errorf("expected the remaining-attempt count, got %q", resp)
	}
	if !strings.Contains(resp, "30 days") {
		t.Errorf("wrong-PIN screen dropped the loan terms: %q", resp)
	}
	if strings.HasPrefix(resp, "END") {
		t.Errorf("a wrong PIN must not end the session: %q", resp)
	}
}

// Exhausting the attempt budget ends the session rather than looping.
func TestLoanConfirm_LockoutEndsTheFlow(t *testing.T) {
	h := newConfirmHarness(t, &pinStub{fakePINSvc: &fakePINSvc{hasPIN: true}, ok: false, remaining: 0})
	session := confirmSession()

	resp, err := h.handleLoanConfirm(context.Background(), session, "4321")
	if err != nil {
		t.Fatalf("handleLoanConfirm: %v", err)
	}
	if strings.Contains(resp, "Enter PIN") {
		t.Errorf("a locked account must not be re-prompted for a PIN: %q", resp)
	}
}

// "0" is the global back input and must reach the payout picker rather than
// being consumed as a PIN attempt.
func TestLoanConfirm_ZeroStepsBackToPayoutMethod(t *testing.T) {
	h := newConfirmHarness(t, &pinStub{fakePINSvc: &fakePINSvc{hasPIN: true}, ok: true, remaining: 3})
	session := confirmSession()
	ctx := context.Background()
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		t.Fatal(err)
	}

	resp, handled, err := h.handleNavigation(ctx, session, navBackInput)
	if err != nil {
		t.Fatalf("handleNavigation: %v", err)
	}
	if !handled {
		t.Fatal("\"0\" should be intercepted as back navigation before the PIN handler sees it")
	}
	if session.CurrentMenu != "payout_method" {
		t.Errorf("expected to step back to payout_method, got %q", session.CurrentMenu)
	}
	if !strings.HasPrefix(resp, "CON ") {
		t.Errorf("expected the payout menu, got %q", resp)
	}
}

// The confirmation screen now carries a PIN, so it must be redacted in logs.
// Without this the merge would leak PINs that the old pin_verify_loan screen
// kept out of them.
func TestLoanConfirm_InputIsRedactedInLogs(t *testing.T) {
	if got := safeInput("loan_confirm", "4321"); got != "[REDACTED]" {
		t.Errorf("safeInput(loan_confirm) = %q, want [REDACTED] — the screen accepts a PIN", got)
	}
}

// Africa's Talking truncates at 160 characters. The merged screen carries terms,
// prompt and navigation hint, so it has the least headroom of any screen.
func TestLoanConfirm_FitsOneUSSDScreen(t *testing.T) {
	h := newConfirmHarness(t, &pinStub{fakePINSvc: &fakePINSvc{hasPIN: true}, ok: false, remaining: 2})
	ctx := context.Background()

	for _, lang := range []string{"en", "sw", "fr"} {
		session := confirmSession()
		session.Language = lang

		// Worst case: the wrong-PIN warning stacked above the terms.
		resp, err := h.handleLoanConfirm(ctx, session, "4321")
		if err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		body := strings.TrimPrefix(resp, "CON ")
		if n := len([]rune(body)); n > 160 {
			t.Errorf("%s: worst-case screen is %d chars, over the 160 limit:\n%s", lang, n, body)
		}
	}
}
