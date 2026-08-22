package notifications

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/contracts"
)

type fakeNotifier struct {
	to  string
	msg string
	err error
}

func (f *fakeNotifier) Send(_ context.Context, to, msg string) error {
	f.to, f.msg = to, msg
	return f.err
}

// newLoanNotifier builds a notifier and fails the test if construction errors.
func newLoanNotifier(t *testing.T, fn Notifier, opts ...LoanOption) *SMSLoanNotifier {
	t.Helper()
	n, err := NewSMSLoanNotifier(fn, opts...)
	if err != nil {
		t.Fatalf("NewSMSLoanNotifier: %v", err)
	}
	return n
}

func TestLoanApproved_English(t *testing.T) {
	fn := &fakeNotifier{}
	n := newLoanNotifier(t, fn)
	note := contracts.LoanNotification{
		PhoneNumber: "254711000111", DisplayCurrency: "KES", DisplayAmount: 5000, LoanReference: "LR-1",
	}
	if err := n.NotifyLoanApproved(context.Background(), note); err != nil {
		t.Fatalf("err: %v", err)
	}
	if fn.to != "254711000111" {
		t.Errorf("to = %q", fn.to)
	}
	if !strings.Contains(fn.msg, "Congratulations") || !strings.Contains(fn.msg, "LR-1") || !strings.Contains(fn.msg, "5000.00") {
		t.Errorf("approved msg missing expected content: %q", fn.msg)
	}
}

func TestLanguageDispatch(t *testing.T) {
	render := func(lang string) string {
		fn := &fakeNotifier{}
		n := newLoanNotifier(t, fn)
		_ = n.NotifyLoanApproved(context.Background(), contracts.LoanNotification{
			Language: lang, DisplayCurrency: "KES", DisplayAmount: 100, LoanReference: "LR-9",
		})
		return fn.msg
	}
	en, sw, fr := render("en"), render("sw"), render("fr")
	if en == sw || en == fr || sw == fr {
		t.Errorf("languages should differ: en=%q sw=%q fr=%q", en, sw, fr)
	}
	// Unknown language falls back to English.
	if render("de") != en {
		t.Error("unknown language should fall back to English")
	}
}

func TestLanguageResolverFallback(t *testing.T) {
	fn := &fakeNotifier{}
	n := newLoanNotifier(t, fn, WithLoanLanguageResolver(
		func(context.Context, string) string { return "fr" },
	))

	// Language left empty -> resolver picks fr.
	_ = n.NotifyLoanApproved(context.Background(), contracts.LoanNotification{
		DisplayCurrency: "KES", DisplayAmount: 1, LoanReference: "LR-2",
	})
	resolved := fn.msg
	// Pinned language wins over the resolver.
	_ = n.NotifyLoanApproved(context.Background(), contracts.LoanNotification{
		Language: "en", DisplayCurrency: "KES", DisplayAmount: 1, LoanReference: "LR-2",
	})
	if resolved == fn.msg {
		t.Error("resolver-selected (fr) and pinned (en) messages should differ")
	}
}

// A builder overriding one message must not lose the other twelve.
func TestPartialTemplateOverrideMerges(t *testing.T) {
	fn := &fakeNotifier{}
	override := &LoanTemplates{
		Rejected: func(n contracts.LoanNotification) string {
			return "DECLINED " + n.LoanReference
		},
	}
	n := newLoanNotifier(t, fn, WithLoanTemplates("en", override))
	note := contracts.LoanNotification{
		Language: "en", DisplayCurrency: "KES", DisplayAmount: 2, LoanReference: "LR-3",
	}

	if err := n.NotifyLoanRejected(context.Background(), note); err != nil {
		t.Fatalf("err: %v", err)
	}
	if fn.msg != "DECLINED LR-3" {
		t.Errorf("override not used, got %q", fn.msg)
	}

	// Approved was left nil, so it falls through to the platform default.
	if err := n.NotifyLoanApproved(context.Background(), note); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(fn.msg, "Congratulations") {
		t.Errorf("unset field should keep the default, got %q", fn.msg)
	}
}

// An override registered for one language must leave the others alone.
func TestOverrideIsPerLanguage(t *testing.T) {
	fn := &fakeNotifier{}
	n := newLoanNotifier(t, fn, WithLoanTemplates("sw", &LoanTemplates{
		Approved: func(contracts.LoanNotification) string { return "SW-OVERRIDE" },
	}))

	_ = n.NotifyLoanApproved(context.Background(), contracts.LoanNotification{Language: "sw"})
	if fn.msg != "SW-OVERRIDE" {
		t.Errorf("sw override not used, got %q", fn.msg)
	}
	_ = n.NotifyLoanApproved(context.Background(), contracts.LoanNotification{Language: "en"})
	if fn.msg == "SW-OVERRIDE" {
		t.Error("sw override leaked into en")
	}
}

func TestConstructorRejectsNonGSM7Override(t *testing.T) {
	_, err := NewSMSLoanNotifier(&fakeNotifier{}, WithLoanTemplates("fr", &LoanTemplates{
		Approved: func(contracts.LoanNotification) string { return "Reçu" },
	}))
	if err == nil {
		t.Fatal("expected an error for copy outside GSM 03.38")
	}
	if !strings.Contains(err.Error(), "Approved") {
		t.Errorf("error should name the offending field, got %v", err)
	}
}

func TestConstructorRejectsEmptyRender(t *testing.T) {
	_, err := NewSMSLoanNotifier(&fakeNotifier{}, WithLoanTemplates("en", &LoanTemplates{
		Approved: func(contracts.LoanNotification) string { return "" },
	}))
	if err == nil {
		t.Fatal("expected an error for a template rendering nothing")
	}
}

func TestConstructorRejectsUnknownLanguage(t *testing.T) {
	_, err := NewSMSLoanNotifier(&fakeNotifier{}, WithLoanTemplates("de", &LoanTemplates{}))
	if err == nil {
		t.Fatal("expected an error for an unsupported language")
	}
}

func TestRepaymentReminderBranches(t *testing.T) {
	fn := &fakeNotifier{}
	n := newLoanNotifier(t, fn)
	base := contracts.LoanNotification{DisplayCurrency: "KES", DisplayAmount: 10, LoanReference: "LR-4"}

	overdue := base
	overdue.DueDate = ptr(time.Now().Add(-24 * time.Hour))
	_ = n.NotifyRepaymentReminder(context.Background(), overdue)
	if !strings.Contains(fn.msg, "URGENT") {
		t.Errorf("overdue msg = %q", fn.msg)
	}

	soon := base
	soon.DueDate = ptr(time.Now().Add(50 * time.Hour))
	_ = n.NotifyRepaymentReminder(context.Background(), soon)
	if !strings.Contains(fn.msg, "due in") {
		t.Errorf("soon msg = %q", fn.msg)
	}

	upcoming := base
	upcoming.DueDate = ptr(time.Now().Add(10 * 24 * time.Hour))
	_ = n.NotifyRepaymentReminder(context.Background(), upcoming)
	if !strings.Contains(fn.msg, "due on") {
		t.Errorf("upcoming msg = %q", fn.msg)
	}
}

func TestRepaymentReminder_NilDueDate(t *testing.T) {
	fn := &fakeNotifier{msg: "sentinel"}
	n := newLoanNotifier(t, fn)
	if err := n.NotifyRepaymentReminder(context.Background(), contracts.LoanNotification{}); err != nil {
		t.Fatalf("err: %v", err)
	}
	if fn.msg != "sentinel" {
		t.Error("nil DueDate should send nothing")
	}
}

func TestCashPickupReady_Content(t *testing.T) {
	fn := &fakeNotifier{}
	n := newLoanNotifier(t, fn)
	_ = n.NotifyLoanCashPickupReady(context.Background(), contracts.LoanNotification{
		DisplayCurrency: "KES", DisplayAmount: 3010, CashPickupRef: "79342377",
		LoanReference: "LR-5", CashPickupInfoURL: "https://mgv.link/x",
	})
	for _, want := range []string{"79342377", "LR-5", "https://mgv.link/x", "3010.00"} {
		if !strings.Contains(fn.msg, want) {
			t.Errorf("cash-pickup-ready msg missing %q: %q", want, fn.msg)
		}
	}
}

func TestSendErrorPropagates(t *testing.T) {
	fn := &fakeNotifier{err: errors.New("transport down")}
	n := newLoanNotifier(t, fn)
	if err := n.NotifyLoanApproved(context.Background(), contracts.LoanNotification{}); err == nil {
		t.Error("expected transport error to propagate")
	}
}

func ptr[T any](v T) *T { return &v }
