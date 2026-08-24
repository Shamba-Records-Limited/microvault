package ussd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	pinPkg "github.com/Shamba-Records-Limited/microvault/pkg/pin"
	stellartypes "github.com/Shamba-Records-Limited/microvault/pkg/stellar/types"
)

// The PIN handlers decide whether to show the account-locked screen. They used
// to do it with strings.Contains(err.Error(), "locked"), which failed both
// ways: rewording pin.ErrAccountLocked silently removed the screen, and any
// unrelated error whose text happened to contain "locked" wrongly triggered it.
// stellartypes.ErrSharesLocked ("shares are locked") is exactly such an error
// and is reachable from this codebase.
//
// These tests pin the behaviour to errors.Is so neither can happen again.

// pinVerifyHandlers are the entry points that gate on a verified PIN.
//
// Repayment used to be one of them. The gate was dropped: nothing leaves the
// borrower's wallet as a result of the USSD session, and a forgotten PIN must
// not become a reason not to repay.
func pinVerifyHandlers(h *USSDHandler) map[string]func(context.Context, *Session, string) (string, error) {
	return map[string]func(context.Context, *Session, string) (string, error){
		"loan":       h.handlePINVerifyLoan,
		"change_pin": h.handlePINChangeOld,
	}
}

func lockedTestSession() *Session {
	return &Session{
		SessionID:   "sess-locked",
		PhoneNumber: "254711000111",
		UserID:      "u1",
		Language:    "en",
		CurrentMenu: "pin_verify",
		Data:        map[string]any{},
	}
}

// isLockedScreen reports whether the response is the account-locked message
// rather than the generic error message.
func isLockedScreen(resp string) bool {
	return strings.Contains(resp, "Account locked")
}

func TestPINVerify_ShowsLockedScreen_OnErrAccountLocked(t *testing.T) {
	// The sentinel as the service actually returns it: wrapped, with the lock
	// duration appended.
	wrapped := fmt.Errorf("%w: try again after 5 minutes", pinPkg.ErrAccountLocked)

	for name, handler := range pinVerifyHandlers(newHarness(t, &fakeUserSvc{user: map[string]any{"id": "u1"}}, &fakePINSvc{hasPIN: true, verifyErr: wrapped})) {
		t.Run(name, func(t *testing.T) {
			resp, err := handler(context.Background(), lockedTestSession(), "1234")
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if !isLockedScreen(resp) {
				t.Errorf("expected the account-locked screen, got %q", resp)
			}
		})
	}
}

func TestPINVerify_DoesNotShowLockedScreen_ForUnrelatedLockedError(t *testing.T) {
	// "shares are locked" contains the substring the old check matched on, but
	// has nothing to do with the borrower's PIN.
	unrelated := fmt.Errorf("vault call failed: %w", stellartypes.ErrSharesLocked)

	if !strings.Contains(unrelated.Error(), "locked") {
		t.Fatal("test premise broken: the decoy error no longer contains \"locked\"")
	}

	for name, handler := range pinVerifyHandlers(newHarness(t, &fakeUserSvc{user: map[string]any{"id": "u1"}}, &fakePINSvc{hasPIN: true, verifyErr: unrelated})) {
		t.Run(name, func(t *testing.T) {
			resp, err := handler(context.Background(), lockedTestSession(), "1234")
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if isLockedScreen(resp) {
				t.Errorf("unrelated error was treated as a PIN lockout: %q", resp)
			}
		})
	}
}

// TestErrAccountLocked_IsMatchable guards the sentinel itself. The handlers can
// only use errors.Is if the service keeps wrapping rather than reformatting.
func TestErrAccountLocked_IsMatchable(t *testing.T) {
	wrapped := fmt.Errorf("%w: try again after 5 minutes", pinPkg.ErrAccountLocked)
	if !errors.Is(wrapped, pinPkg.ErrAccountLocked) {
		t.Fatal("pin.ErrAccountLocked is no longer matchable through wrapping")
	}
}
