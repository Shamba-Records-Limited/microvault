package ussd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Shamba-Records-Limited/microvault/pkg/notifications"
)

func consentHarness(t *testing.T, ask bool) (*USSDHandler, *fakeUserSvc) {
	t.Helper()
	user := &fakeUserSvc{getErr: errors.New("not found")}
	h := newHarness(t, user, &fakePINSvc{})
	h.askCreditConsent = ask
	return h, user
}

func run(t *testing.T, h *USSDHandler, sess string, inputs ...string) string {
	t.Helper()
	var last string
	for i, in := range inputs {
		resp, err := h.HandleRequest(context.Background(), sess, "254711000111", "*384#", "63902", in)
		if err != nil {
			t.Fatalf("step %d (input %q): %v", i, in, err)
		}
		last = resp
	}
	return last
}

func TestConsent_AcceptedIsSentWithRegistration(t *testing.T) {
	h, user := consentHarness(t, true)
	before := time.Now().Add(-time.Second)

	resp := run(t, h, "c1", "", "1", "Jane Wanjiku", "12345678")
	if !strings.Contains(resp, "credit partner") {
		t.Fatalf("consent screen not shown after the national ID: %q", resp)
	}
	run(t, h, "c1", "1", "2846", "2846")

	if !user.registered {
		t.Fatal("RegisterUser was never called")
	}
	at := user.lastReq.CreditConsentAt
	if at == nil || at.Before(before) || at.After(time.Now()) {
		t.Fatalf("CreditConsentAt = %v, want the time of acceptance", at)
	}
}

func TestConsent_DeclinedStillRegisters(t *testing.T) {
	h, user := consentHarness(t, true)
	run(t, h, "c2", "", "1", "Jane Wanjiku", "12345678", "2", "2846", "2846")
	if !user.registered {
		t.Fatal("declining consent must not block registration")
	}
	if user.lastReq.CreditConsentAt != nil {
		t.Fatalf("CreditConsentAt = %v, want nil after declining", user.lastReq.CreditConsentAt)
	}
}

func TestConsent_InvalidChoiceRePrompts(t *testing.T) {
	h, user := consentHarness(t, true)
	resp := run(t, h, "c3", "", "1", "Jane Wanjiku", "12345678", "7")
	if !strings.HasPrefix(resp, "CON ") || !strings.Contains(resp, "credit partner") {
		t.Fatalf("an invalid choice should re-ask: %q", resp)
	}
	if user.registered {
		t.Fatal("registered without an answer")
	}
}

func TestConsent_BackReturnsToNationalIDAndForgetsTheAnswer(t *testing.T) {
	h, user := consentHarness(t, true)
	run(t, h, "c4", "", "1", "Jane Wanjiku", "12345678")
	resp := run(t, h, "c4", navBackInput)
	if !strings.Contains(resp, GetLocalizedMessage("en", "reg_enter_national_id")) {
		t.Fatalf("back from consent should re-ask the national ID: %q", resp)
	}
	run(t, h, "c4", "12345678", "1")
	resp = run(t, h, "c4", navBackInput)
	if !strings.Contains(resp, GetLocalizedMessage("en", "reg_enter_national_id")) {
		t.Fatalf("back from PIN creation should re-ask the national ID: %q", resp)
	}
	run(t, h, "c4", "12345678", "2", "2846", "2846")
	if user.lastReq.CreditConsentAt != nil {
		t.Fatal("an answer the user went back over must not survive")
	}
}

func TestConsent_NotAskedWhenOff(t *testing.T) {
	h, user := consentHarness(t, false)
	resp := run(t, h, "c5", "", "1", "Jane Wanjiku", "12345678")
	if strings.Contains(resp, "credit partner") {
		t.Fatalf("consent asked while the integration is off: %q", resp)
	}
	run(t, h, "c5", "2846", "2846")
	if !user.registered || user.lastReq.CreditConsentAt != nil {
		t.Fatalf("registered=%v consent=%v, want registered with no consent", user.registered, user.lastReq.CreditConsentAt)
	}
}

func TestConsentString_IsGSM7AndFits(t *testing.T) {
	for _, lang := range []string{"en", "sw", "fr"} {
		msg := GetLocalizedMessage(lang, "reg_credit_consent")
		if _, bad, ok := notifications.GSM7Len(msg); !ok {
			t.Errorf("%s carries non-GSM character %q", lang, bad)
		}
		if n := utf8.RuneCountInString(msg); n > navHintBudget {
			t.Errorf("%s is %d characters, over the %d-character screen", lang, n, navHintBudget)
		}
	}
}
