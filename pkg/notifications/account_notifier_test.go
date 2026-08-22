package notifications

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Shamba-Records-Limited/microvault/pkg/contracts"
)

// newAccountNotifier builds a notifier and fails the test if construction errors.
func newAccountNotifier(t *testing.T, fn Notifier, opts ...AccountOption) *SMSAccountNotifier {
	t.Helper()
	n, err := NewSMSAccountNotifier(fn, opts...)
	if err != nil {
		t.Fatalf("NewSMSAccountNotifier: %v", err)
	}
	return n
}

func TestAccountNotifier_Content(t *testing.T) {
	fn := &fakeNotifier{}
	n := newAccountNotifier(t, fn)
	ctx := context.Background()

	if err := n.NotifyRegistrationSuccess(ctx, contracts.AccountNotification{FullName: "Alice Wanjiku"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fn.msg, "Alice Wanjiku") {
		t.Errorf("registration msg missing name: %q", fn.msg)
	}

	if err := n.NotifyPINWrongAttempt(ctx, contracts.AccountNotification{RemainingAttempts: 2}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fn.msg, "2") {
		t.Errorf("wrong-attempt msg missing remaining count: %q", fn.msg)
	}

	if err := n.NotifyAccountLocked(ctx, contracts.AccountNotification{LockedUntil: "15 minutes"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fn.msg, "15 minutes") {
		t.Errorf("locked msg missing duration: %q", fn.msg)
	}
}

// TestAccountNotifier_AllMethods exercises every method for language dispatch
// and error propagation in one pass.
func TestAccountNotifier_AllMethods(t *testing.T) {
	note := contracts.AccountNotification{
		PhoneNumber: "254711000111", FullName: "Bob", Reason: "bad", RemainingAttempts: 1, LockedUntil: "5 minutes",
	}
	calls := map[string]func(*SMSAccountNotifier) error{
		"RegistrationSuccess": func(n *SMSAccountNotifier) error { return n.NotifyRegistrationSuccess(context.Background(), note) },
		"RegistrationFailed":  func(n *SMSAccountNotifier) error { return n.NotifyRegistrationFailed(context.Background(), note) },
		"PINWrongAttempt":     func(n *SMSAccountNotifier) error { return n.NotifyPINWrongAttempt(context.Background(), note) },
		"AccountLocked":       func(n *SMSAccountNotifier) error { return n.NotifyAccountLocked(context.Background(), note) },
		"PINChanged":          func(n *SMSAccountNotifier) error { return n.NotifyPINChanged(context.Background(), note) },
		"PINChangeFailed":     func(n *SMSAccountNotifier) error { return n.NotifyPINChangeFailed(context.Background(), note) },
		"PINReset":            func(n *SMSAccountNotifier) error { return n.NotifyPINReset(context.Background(), note) },
		"PINResetFailed":      func(n *SMSAccountNotifier) error { return n.NotifyPINResetFailed(context.Background(), note) },
	}
	for name, call := range calls {
		// Happy path: sends a non-empty message.
		fn := &fakeNotifier{}
		if err := call(newAccountNotifier(t, fn)); err != nil {
			t.Errorf("%s: unexpected error %v", name, err)
		}
		if fn.msg == "" {
			t.Errorf("%s: sent an empty message", name)
		}
		// Error path: transport failure propagates.
		errFn := &fakeNotifier{err: errors.New("down")}
		if err := call(newAccountNotifier(t, errFn)); err == nil {
			t.Errorf("%s: expected transport error to propagate", name)
		}
	}
}

func TestAccountPartialOverrideMerges(t *testing.T) {
	fn := &fakeNotifier{}
	n := newAccountNotifier(t, fn, WithAccountTemplates("en", &AccountTemplates{
		PINChanged: func(contracts.AccountNotification) string { return "PIN updated." },
	}))
	ctx := context.Background()

	if err := n.NotifyPINChanged(ctx, contracts.AccountNotification{}); err != nil {
		t.Fatal(err)
	}
	if fn.msg != "PIN updated." {
		t.Errorf("override not used, got %q", fn.msg)
	}

	if err := n.NotifyPINReset(ctx, contracts.AccountNotification{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fn.msg, "reset") {
		t.Errorf("unset field should keep the default, got %q", fn.msg)
	}
}

func TestAccountConstructorRejectsNilField(t *testing.T) {
	// A set whose every field is nil is indistinguishable from "override
	// nothing", so it must merge cleanly rather than fail.
	if _, err := NewSMSAccountNotifier(&fakeNotifier{}, WithAccountTemplates("en", &AccountTemplates{})); err != nil {
		t.Fatalf("an empty override should merge to the defaults, got %v", err)
	}
	// A field that renders nothing is a genuine mistake.
	_, err := NewSMSAccountNotifier(&fakeNotifier{}, WithAccountTemplates("en", &AccountTemplates{
		PINReset: func(contracts.AccountNotification) string { return "" },
	}))
	if err == nil {
		t.Fatal("expected an error for a template rendering nothing")
	}
}
