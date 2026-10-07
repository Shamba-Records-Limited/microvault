package ussd

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Shamba-Records-Limited/microvault/pkg/notifications"
)

type fakePilotGate struct {
	phoneActive bool
	nationalID  string
	userActive  bool
	err         error
}

func (g *fakePilotGate) PhoneActive(context.Context, string) (bool, error) {
	return g.phoneActive, g.err
}

func (g *fakePilotGate) Matches(_ context.Context, _, nationalID string) (bool, error) {
	return g.phoneActive && nationalID == g.nationalID, g.err
}

func (g *fakePilotGate) UserActive(context.Context, string) (bool, error) {
	return g.userActive, g.err
}

const pilotPhone = "254711000111"

func dial(t *testing.T, h *USSDHandler, sess string, inputs ...string) string {
	t.Helper()
	var resp string
	for _, in := range inputs {
		var err error
		resp, err = h.HandleRequest(context.Background(), sess, pilotPhone, "*384#", "63902", in)
		if err != nil {
			t.Fatalf("input %q: %v", in, err)
		}
	}
	return resp
}

func TestPilotGate_UnlistedPhoneEndsAtFirstDial(t *testing.T) {
	h := newHarness(t, &fakeUserSvc{getErr: errors.New("not found")}, &fakePINSvc{})
	h.pilotGate = &fakePilotGate{}

	resp := dial(t, h, "p1", "")
	if resp != "END "+GetLocalizedMessage("en", "pilot_closed") {
		t.Errorf("expected pilot_closed, got %q", resp)
	}
}

func TestPilotGate_LookupErrorFailsClosed(t *testing.T) {
	h := newHarness(t, &fakeUserSvc{getErr: errors.New("not found")}, &fakePINSvc{})
	h.pilotGate = &fakePilotGate{phoneActive: true, err: errors.New("db down")}

	resp := dial(t, h, "p2", "")
	if !strings.HasPrefix(resp, "END ") {
		t.Errorf("a failed lookup must end the session, got %q", resp)
	}
}

func TestPilotGate_MatchingIDRegisters(t *testing.T) {
	user := &fakeUserSvc{getErr: errors.New("not found")}
	h := newHarness(t, user, &fakePINSvc{})
	h.pilotGate = &fakePilotGate{phoneActive: true, nationalID: "12345678"}

	resp := dial(t, h, "p3", "", "1", "Jane Doe", "12345678", "2846", "2846")
	if !user.registered {
		t.Fatalf("expected registration, last response %q", resp)
	}
}

func TestPilotGate_WrongIDRepromptsThenEnds(t *testing.T) {
	user := &fakeUserSvc{getErr: errors.New("not found")}
	h := newHarness(t, user, &fakePINSvc{})
	h.pilotGate = &fakePilotGate{phoneActive: true, nationalID: "12345678"}

	resp := dial(t, h, "p4", "", "1", "Jane Doe", "99999999")
	if !strings.HasPrefix(resp, "CON ") || !strings.Contains(resp, "does not match") {
		t.Fatalf("expected the mismatch re-prompt, got %q", resp)
	}
	if strings.Contains(resp, "12345678") {
		t.Errorf("the re-prompt must not reveal the approved ID: %q", resp)
	}

	resp = dial(t, h, "p4", "88888888")
	if resp != "END "+GetLocalizedMessage("en", "pilot_closed") {
		t.Errorf("expected pilot_closed after the second miss, got %q", resp)
	}
	if user.registered {
		t.Error("a mismatched ID must not register")
	}
}

func TestPilotGate_RevokedMidSessionDoesNotRegister(t *testing.T) {
	user := &fakeUserSvc{getErr: errors.New("not found")}
	h := newHarness(t, user, &fakePINSvc{})
	gate := &fakePilotGate{phoneActive: true, nationalID: "12345678"}
	h.pilotGate = gate

	dial(t, h, "p5", "", "1", "Jane Doe", "12345678", "2846")
	gate.phoneActive = false
	resp := dial(t, h, "p5", "2846")
	if resp != "END "+GetLocalizedMessage("en", "pilot_closed") {
		t.Errorf("expected pilot_closed, got %q", resp)
	}
	if user.registered {
		t.Error("a row revoked mid-session must not register")
	}
}

func TestPilotGate_RevokedRegisteredUserIsSuspended(t *testing.T) {
	user := &fakeUserSvc{user: map[string]any{"id": "u1"}, accounts: []any{map[string]any{}}}
	h := newHarness(t, user, &fakePINSvc{hasPIN: true})
	h.pilotGate = &fakePilotGate{}

	resp := dial(t, h, "p6", "")
	if resp != "END "+GetLocalizedMessage("en", "pilot_suspended") {
		t.Errorf("expected pilot_suspended, got %q", resp)
	}
}

func TestPilotGate_ActiveRegisteredUserSeesMainMenu(t *testing.T) {
	user := &fakeUserSvc{user: map[string]any{"id": "u1"}, accounts: []any{map[string]any{}}}
	h := newHarness(t, user, &fakePINSvc{hasPIN: true})
	h.pilotGate = &fakePilotGate{userActive: true}

	resp := dial(t, h, "p7", "")
	if !strings.Contains(resp, "Request Loan") {
		t.Errorf("expected the main menu, got %q", resp)
	}
}

func TestPilotStrings_AreGSM7(t *testing.T) {
	for _, key := range []string{"pilot_closed", "pilot_id_mismatch", "pilot_suspended", "loan_cash_pickup_min_amount", "loan_cash_pickup_max_amount"} {
		for _, lang := range []string{"en", "sw", "fr"} {
			if _, bad, ok := notifications.GSM7Len(GetLocalizedMessage(lang, key)); !ok {
				t.Errorf("%s/%s carries non-GSM character %q", key, lang, bad)
			}
		}
	}
}

func newBorrowHarness(t *testing.T) *USSDHandler {
	t.Helper()
	user := &fakeUserSvc{user: map[string]any{"id": "u1"}, accounts: []any{map[string]any{}}}
	h := newHarness(t, user, &fakePINSvc{hasPIN: true})
	h.mobileMoneyBorrowOff = true
	return h
}

// At the fake rate of 130 KES/USD, MoneyGram's 15 USD floor is KES 1,950.
func TestMobileMoneyOff_SkipsPayoutMenu(t *testing.T) {
	h := newBorrowHarness(t)

	resp := dial(t, h, "m1", "", "1", "2000")
	if !strings.Contains(resp, "Enter PIN") {
		t.Fatalf("expected the confirmation screen, got %q", resp)
	}
	session, err := h.sessionManager.GetOrCreateSession(context.Background(), "m1", pilotPhone, "*384#", "63902")
	if err != nil {
		t.Fatal(err)
	}
	if got := session.Data["payout_method"]; got != "cash_pickup" {
		t.Errorf("payout_method = %v, want cash_pickup", got)
	}
}

func TestMobileMoneyOff_OutOfCorridorRepromptsAmount(t *testing.T) {
	h := newBorrowHarness(t)

	resp := dial(t, h, "m2", "", "1", "1000")
	if !strings.HasPrefix(resp, "CON ") || !strings.Contains(resp, "1950") || !strings.Contains(resp, "Enter a new amount") {
		t.Fatalf("expected the amount re-prompt with the floor, got %q", resp)
	}
	if strings.Contains(resp, "Mobile Money") {
		t.Errorf("mobile money must not be offered while switched off: %q", resp)
	}

	resp = dial(t, h, "m2", "2000")
	if !strings.Contains(resp, "Enter PIN") {
		t.Errorf("a corrected amount should reach confirmation, got %q", resp)
	}
}

func TestMobileMoneyOff_BackFromConfirmReturnsToAmount(t *testing.T) {
	h := newBorrowHarness(t)

	resp := dial(t, h, "m3", "", "1", "2000", "0")
	if !strings.Contains(resp, "Enter amount to borrow") {
		t.Errorf("expected the amount prompt, got %q", resp)
	}
}
